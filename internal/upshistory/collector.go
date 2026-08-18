package upshistory

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"powercheck/internal/nutnetwork"
)

type Reader interface {
	Read(context.Context) (nutnetwork.Status, error)
}

type Sample struct {
	CheckedAt      time.Time `json:"checked_at"`
	Connected      bool      `json:"connected"`
	UPSStatus      string    `json:"ups_status,omitempty"`
	LoadPercent    *float64  `json:"load_percent,omitempty"`
	ChargePercent  *float64  `json:"charge_percent,omitempty"`
	RuntimeSeconds *int      `json:"runtime_seconds,omitempty"`
	RealPowerWatts *float64  `json:"realpower_watts,omitempty"`
	InputVoltage   *float64  `json:"input_voltage,omitempty"`
	Error          string    `json:"error,omitempty"`
}

type Point struct {
	CheckedAt      time.Time `json:"checked_at"`
	Connected      bool      `json:"connected"`
	LoadPercent    *float64  `json:"load_percent,omitempty"`
	LoadPercentMax *float64  `json:"load_percent_max,omitempty"`
	ChargePercent  *float64  `json:"charge_percent,omitempty"`
	RuntimeSeconds *int      `json:"runtime_seconds,omitempty"`
}

type Assessment struct {
	Level                   string   `json:"level"`
	Title                   string   `json:"title"`
	Reasons                 []string `json:"reasons"`
	TheoreticalEnergyWh     *float64 `json:"theoretical_energy_wh,omitempty"`
	EstimatedUsableEnergyWh *float64 `json:"estimated_usable_energy_wh,omitempty"`
	EstimatedLoadWatts      *float64 `json:"estimated_load_watts,omitempty"`
	EnergyEstimateMethod    string   `json:"energy_estimate_method,omitempty"`
	RuntimeMarginSeconds    *int     `json:"runtime_margin_seconds,omitempty"`
	ShutdownBudgetSeconds   int      `json:"shutdown_budget_seconds"`
	BatteryAgeYears         *float64 `json:"battery_age_years,omitempty"`
	BatteryAgeBasis         string   `json:"battery_age_basis,omitempty"`
	ReplacementBattery      string   `json:"replacement_battery,omitempty"`
	SpecificationSource     string   `json:"specification_source,omitempty"`
}

type Report struct {
	Connected  bool              `json:"connected"`
	Latest     nutnetwork.Status `json:"latest"`
	Points     []Point           `json:"points"`
	Assessment Assessment        `json:"assessment"`
	From       time.Time         `json:"from"`
	To         time.Time         `json:"to"`
}

type Collector struct {
	source    Reader
	filePath  string
	interval  time.Duration
	retention time.Duration
	spec      Spec

	sampleMu   sync.Mutex
	mu         sync.RWMutex
	samples    []Sample
	latest     nutnetwork.Status
	lastErr    error
	persistErr error
	writes     int
}

func NewCollector(source Reader, filePath string, interval, retention time.Duration, spec Spec) (*Collector, error) {
	if source == nil {
		return nil, fmt.Errorf("UPS history source is required")
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	collector := &Collector{
		source:    source,
		filePath:  filePath,
		interval:  interval,
		retention: retention,
		spec:      spec,
	}
	if err := collector.load(); err != nil {
		return nil, err
	}
	return collector, nil
}

func (c *Collector) Start(ctx context.Context) {
	go func() {
		c.sample(ctx)
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.sample(ctx)
			}
		}
	}()
}

func (c *Collector) Read(ctx context.Context) (nutnetwork.Status, error) {
	c.mu.RLock()
	latest := c.latest
	lastErr := c.lastErr
	c.mu.RUnlock()
	if latest.CheckedAt.IsZero() && lastErr == nil {
		c.sample(ctx)
		c.mu.RLock()
		latest = c.latest
		lastErr = c.lastErr
		c.mu.RUnlock()
	}
	if lastErr != nil {
		return nutnetwork.Status{}, lastErr
	}
	if latest.CheckedAt.IsZero() {
		return nutnetwork.Status{}, fmt.Errorf("UPS history has no sample yet")
	}
	return latest, nil
}

func (c *Collector) Report(since time.Time, maxPoints int) Report {
	if maxPoints < 10 {
		maxPoints = 10
	}
	if maxPoints > 2000 {
		maxPoints = 2000
	}
	c.mu.RLock()
	latest := c.latest
	connected := c.lastErr == nil && !latest.CheckedAt.IsZero()
	filtered := make([]Sample, 0, len(c.samples))
	for _, sample := range c.samples {
		if !sample.CheckedAt.Before(since) {
			filtered = append(filtered, sample)
		}
	}
	c.mu.RUnlock()
	now := time.Now().UTC()
	return Report{
		Connected:  connected,
		Latest:     latest,
		Points:     downsample(filtered, maxPoints),
		Assessment: Assess(latest, c.spec, now),
		From:       since,
		To:         now,
	}
}

func (c *Collector) sample(ctx context.Context) {
	c.sampleMu.Lock()
	defer c.sampleMu.Unlock()
	status, err := c.source.Read(ctx)
	now := time.Now().UTC()
	sample := Sample{CheckedAt: now, Connected: err == nil}
	if err != nil {
		sample.Error = err.Error()
	} else {
		sample.CheckedAt = status.CheckedAt
		sample.UPSStatus = status.UPSStatus
		sample.LoadPercent = status.UPSLoadPercent
		sample.ChargePercent = status.BatteryCharge
		sample.RuntimeSeconds = status.BatteryRuntimeSeconds
		sample.RealPowerWatts = status.UPSRealPowerWatts
		sample.InputVoltage = status.InputVoltage
	}

	c.mu.Lock()
	if err == nil {
		c.latest = status
	}
	c.lastErr = err
	c.samples = append(c.samples, sample)
	c.pruneLocked(now.Add(-c.retention))
	c.writes++
	compact := c.writes%120 == 0
	c.mu.Unlock()

	if c.filePath != "" {
		if appendErr := c.append(sample); appendErr != nil {
			c.setPersistenceError(appendErr)
		} else if compact {
			if compactErr := c.compact(); compactErr != nil {
				c.setPersistenceError(compactErr)
			}
		}
	}
}

func (c *Collector) setPersistenceError(err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	c.persistErr = fmt.Errorf("persist UPS history: %w", err)
	c.mu.Unlock()
}

func (c *Collector) load() error {
	if c.filePath == "" {
		return nil
	}
	file, err := os.Open(c.filePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open UPS history: %w", err)
	}
	defer file.Close()
	cutoff := time.Now().UTC().Add(-c.retention)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var sample Sample
		if err := json.Unmarshal(scanner.Bytes(), &sample); err != nil {
			return fmt.Errorf("decode UPS history: %w", err)
		}
		if !sample.CheckedAt.Before(cutoff) {
			c.samples = append(c.samples, sample)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read UPS history: %w", err)
	}
	return nil
}

func (c *Collector) append(sample Sample) error {
	if err := os.MkdirAll(filepath.Dir(c.filePath), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(c.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	encoded, err := json.Marshal(sample)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func (c *Collector) compact() error {
	c.mu.RLock()
	samples := append([]Sample(nil), c.samples...)
	c.mu.RUnlock()
	if err := os.MkdirAll(filepath.Dir(c.filePath), 0o700); err != nil {
		return err
	}
	staged := c.filePath + ".tmp"
	file, err := os.OpenFile(staged, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(file)
	for _, sample := range samples {
		encoded, marshalErr := json.Marshal(sample)
		if marshalErr != nil {
			file.Close()
			return marshalErr
		}
		if _, writeErr := writer.Write(append(encoded, '\n')); writeErr != nil {
			file.Close()
			return writeErr
		}
	}
	if err := writer.Flush(); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(staged, c.filePath)
}

func (c *Collector) pruneLocked(cutoff time.Time) {
	first := 0
	for first < len(c.samples) && c.samples[first].CheckedAt.Before(cutoff) {
		first++
	}
	if first > 0 {
		c.samples = append([]Sample(nil), c.samples[first:]...)
	}
}

func downsample(samples []Sample, maxPoints int) []Point {
	if len(samples) == 0 {
		return []Point{}
	}
	bucketSize := int(math.Ceil(float64(len(samples)) / float64(maxPoints)))
	points := make([]Point, 0, int(math.Ceil(float64(len(samples))/float64(bucketSize))))
	for start := 0; start < len(samples); start += bucketSize {
		end := start + bucketSize
		if end > len(samples) {
			end = len(samples)
		}
		points = append(points, aggregate(samples[start:end]))
	}
	return points
}

func aggregate(samples []Sample) Point {
	last := samples[len(samples)-1]
	point := Point{
		CheckedAt:      last.CheckedAt,
		Connected:      last.Connected,
		ChargePercent:  last.ChargePercent,
		RuntimeSeconds: last.RuntimeSeconds,
	}
	var total, maximum float64
	count := 0
	for _, sample := range samples {
		if sample.LoadPercent == nil {
			continue
		}
		total += *sample.LoadPercent
		if count == 0 || *sample.LoadPercent > maximum {
			maximum = *sample.LoadPercent
		}
		count++
	}
	if count > 0 {
		average := total / float64(count)
		point.LoadPercent = &average
		point.LoadPercentMax = &maximum
	}
	return point
}

func Assess(status nutnetwork.Status, spec Spec, now time.Time) Assessment {
	assessment := Assessment{
		Level:                 "unknown",
		Title:                 "需要更多数据",
		ShutdownBudgetSeconds: spec.ShutdownBudgetSeconds,
		ReplacementBattery:    spec.ReplacementBattery,
		SpecificationSource:   spec.SourceNote,
	}
	voltage := valueOr(status.BatteryVoltageNominal, spec.BatteryVoltage)
	capacity := valueOr(status.BatteryCapacityNominalAH, spec.BatteryCapacityAH)
	if voltage > 0 && capacity > 0 {
		theoretical := voltage * capacity
		assessment.TheoreticalEnergyWh = &theoretical
	}
	loadWatts, method := estimateLoad(status, spec)
	if loadWatts > 0 {
		assessment.EstimatedLoadWatts = &loadWatts
		assessment.EnergyEstimateMethod = method
		if status.BatteryRuntimeSeconds != nil {
			usable := loadWatts * float64(*status.BatteryRuntimeSeconds) / 3600
			assessment.EstimatedUsableEnergyWh = &usable
		}
	}
	if status.BatteryRuntimeSeconds != nil {
		margin := *status.BatteryRuntimeSeconds - spec.ShutdownBudgetSeconds
		assessment.RuntimeMarginSeconds = &margin
	}
	age, basis := batteryAge(status, spec, now)
	if age != nil {
		assessment.BatteryAgeYears = age
		assessment.BatteryAgeBasis = basis
	}

	selfTest := strings.ToLower(status.SelfTestResult)
	batteryState := strings.ToLower(status.BatteryStatus)
	if (status.BatteryPacksBad != nil && *status.BatteryPacksBad > 0) ||
		strings.Contains(selfTest, "failed") ||
		strings.Contains(selfTest, "replace") ||
		strings.Contains(batteryState, "bad") ||
		strings.Contains(batteryState, "replace") {
		assessment.Level = "replace"
		assessment.Title = "建议更换电池"
		assessment.Reasons = append(assessment.Reasons, "UPS 报告了电池或自检异常。")
		return assessment
	}
	if assessment.RuntimeMarginSeconds != nil && *assessment.RuntimeMarginSeconds < 0 {
		assessment.Level = "replace"
		assessment.Title = "建议尽快更换电池或降低负载"
		assessment.Reasons = append(assessment.Reasons, "当前预计续航无法覆盖关机预算。")
	} else if assessment.RuntimeMarginSeconds != nil && *assessment.RuntimeMarginSeconds < 60 {
		assessment.Level = "observe"
		assessment.Title = "建议准备更换并做受控放电测试"
		assessment.Reasons = append(assessment.Reasons, "当前预计续航距关机预算不足 60 秒。")
	} else {
		assessment.Level = "good"
		assessment.Title = "暂不建议仅凭当前数据更换"
	}
	if age != nil && strings.Contains(strings.ToLower(status.BatteryType), "pb") {
		switch {
		case *age >= 5:
			assessment.Level = "observe"
			assessment.Title = "建议准备更换并做受控放电测试"
			assessment.Reasons = append(assessment.Reasons, "铅酸电池记录已达到约 5 年。")
		case *age >= 3:
			assessment.Reasons = append(assessment.Reasons, "铅酸电池记录已超过约 3 年，应关注续航趋势。")
		}
	}
	if strings.Contains(selfTest, "passed") {
		assessment.Reasons = append(assessment.Reasons, "UPS 最近一次自检结果为通过。")
	}
	if assessment.EstimatedUsableEnergyWh != nil {
		assessment.Reasons = append(
			assessment.Reasons,
			"运行时能量是根据当前负载和 UPS 续航估算，不等同于放电测试容量。",
		)
	}
	if len(assessment.Reasons) == 0 {
		assessment.Reasons = append(assessment.Reasons, "NUT 未提供足够的容量或电池健康字段。")
	}
	return assessment
}

func estimateLoad(status nutnetwork.Status, spec Spec) (float64, string) {
	if status.UPSRealPowerWatts != nil && *status.UPSRealPowerWatts > 0 {
		return *status.UPSRealPowerWatts, "NUT 实时有功功率"
	}
	rated := valueOr(status.UPSRealPowerNominalWatts, spec.RatedWatts)
	if rated > 0 && status.UPSLoadPercent != nil {
		return rated * *status.UPSLoadPercent / 100, "额定瓦数 × NUT 负载百分比"
	}
	return 0, ""
}

func batteryAge(status nutnetwork.Status, spec Spec, now time.Time) (*float64, string) {
	if installed, err := time.Parse("2006-01-02", spec.BatteryInstalledDate); err == nil {
		years := now.Sub(installed).Hours() / (24 * 365.2425)
		return &years, "配置的电池安装日期"
	}
	for _, candidate := range []struct {
		value string
		basis string
	}{
		{status.BatteryReplacementDate, "NUT 电池更换日期"},
		{status.BatteryManufacturedDate, "NUT 电池生产日期"},
	} {
		if parsed, ok := parseDate(candidate.value, now); ok {
			years := now.Sub(parsed).Hours() / (24 * 365.2425)
			return &years, candidate.basis
		}
	}
	if parsed, ok := parseDate(status.UPSManufacturedDate, now); ok {
		years := now.Sub(parsed).Hours() / (24 * 365.2425)
		return &years, "UPS 生产日期（仅作为电池年龄上限）"
	}
	return nil, ""
}

func parseDate(value string, now time.Time) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, format := range []string{"2006/01/02", "2006-01-02", "01/02/06"} {
		parsed, err := time.Parse(format, value)
		if err == nil && parsed.Year() >= 2005 && !parsed.After(now) {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func valueOr(value *float64, fallback float64) float64 {
	if value != nil && *value > 0 {
		return *value
	}
	return fallback
}
