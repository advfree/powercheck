package upshistory

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type Spec struct {
	Model                 string  `json:"model"`
	RatedWatts            float64 `json:"rated_watts"`
	BatteryVoltage        float64 `json:"battery_voltage"`
	BatteryCapacityAH     float64 `json:"battery_capacity_ah"`
	ReplacementBattery    string  `json:"replacement_battery,omitempty"`
	BatteryInstalledDate  string  `json:"battery_installed_date,omitempty"`
	ShutdownBudgetSeconds int     `json:"shutdown_budget_seconds"`
	SourceNote            string  `json:"source_note,omitempty"`
}

func LoadSpec(filePath string) (Spec, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return Spec{}, fmt.Errorf("open UPS spec: %w", err)
	}
	defer file.Close()
	var spec Spec
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return Spec{}, fmt.Errorf("decode UPS spec: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

func (s *Spec) Validate() error {
	s.Model = strings.TrimSpace(s.Model)
	s.ReplacementBattery = strings.TrimSpace(s.ReplacementBattery)
	s.BatteryInstalledDate = strings.TrimSpace(s.BatteryInstalledDate)
	s.SourceNote = strings.TrimSpace(s.SourceNote)
	if s.RatedWatts < 0 || s.RatedWatts > 100000 {
		return fmt.Errorf("UPS rated watts is outside the supported range")
	}
	if s.BatteryVoltage < 0 || s.BatteryVoltage > 1000 {
		return fmt.Errorf("battery voltage is outside the supported range")
	}
	if s.BatteryCapacityAH < 0 || s.BatteryCapacityAH > 10000 {
		return fmt.Errorf("battery capacity is outside the supported range")
	}
	if s.ShutdownBudgetSeconds == 0 {
		s.ShutdownBudgetSeconds = 120
	}
	if s.ShutdownBudgetSeconds < 30 || s.ShutdownBudgetSeconds > 3600 {
		return fmt.Errorf("shutdown budget must be between 30 and 3600 seconds")
	}
	if s.BatteryInstalledDate != "" {
		if _, err := time.Parse("2006-01-02", s.BatteryInstalledDate); err != nil {
			return fmt.Errorf("battery installed date must use YYYY-MM-DD")
		}
	}
	return nil
}
