package upshistory

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"powercheck/internal/nutnetwork"
)

type readerStub struct {
	status nutnetwork.Status
	err    error
}

func (s readerStub) Read(context.Context) (nutnetwork.Status, error) {
	return s.status, s.err
}

func floatPointer(value float64) *float64 {
	return &value
}

func intPointer(value int) *int {
	return &value
}

func TestSpecDefaultsToActiveShutdownBudget(t *testing.T) {
	t.Parallel()
	spec := Spec{}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	if spec.ShutdownBudgetSeconds != 120 {
		t.Fatalf("shutdown budget=%d, want 120", spec.ShutdownBudgetSeconds)
	}
}

func TestAssessmentSeparatesRatedAndRuntimeEnergy(t *testing.T) {
	t.Parallel()
	status := nutnetwork.Status{
		Model:                    "Back-UPS BK650M2-CH",
		UPSLoadPercent:           floatPointer(55),
		UPSRealPowerNominalWatts: floatPointer(390),
		BatteryCharge:            floatPointer(100),
		BatteryRuntimeSeconds:    intPointer(326),
		BatteryVoltageNominal:    floatPointer(12),
		BatteryType:              "PbAc",
		BatteryManufacturedDate:  "2001/01/01",
		UPSManufacturedDate:      "2021/11/20",
		SelfTestResult:           "Done and passed",
	}
	spec := Spec{
		BatteryCapacityAH:     4.2,
		ShutdownBudgetSeconds: 300,
		ReplacementBattery:    "APCRBC153",
	}
	assessment := Assess(status, spec, time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC))
	if assessment.TheoreticalEnergyWh == nil ||
		math.Abs(*assessment.TheoreticalEnergyWh-50.4) > 0.01 {
		t.Fatalf("unexpected theoretical energy: %#v", assessment.TheoreticalEnergyWh)
	}
	if assessment.EstimatedLoadWatts == nil ||
		math.Abs(*assessment.EstimatedLoadWatts-214.5) > 0.01 {
		t.Fatalf("unexpected load estimate: %#v", assessment.EstimatedLoadWatts)
	}
	if assessment.EstimatedUsableEnergyWh == nil ||
		math.Abs(*assessment.EstimatedUsableEnergyWh-19.4225) > 0.01 {
		t.Fatalf("unexpected usable energy: %#v", assessment.EstimatedUsableEnergyWh)
	}
	if assessment.RuntimeMarginSeconds == nil || *assessment.RuntimeMarginSeconds != 26 {
		t.Fatalf("unexpected runtime margin: %#v", assessment.RuntimeMarginSeconds)
	}
	if assessment.Level != "observe" {
		t.Fatalf("unexpected assessment: %#v", assessment)
	}
	if assessment.BatteryAgeBasis != "UPS 生产日期（仅作为电池年龄上限）" {
		t.Fatalf("placeholder battery date was not ignored: %#v", assessment)
	}
}

func TestCollectorPersistsAndReportsSamples(t *testing.T) {
	t.Parallel()
	checkedAt := time.Now().UTC()
	filePath := filepath.Join(t.TempDir(), "history", "ups.jsonl")
	collector, err := NewCollector(
		readerStub{status: nutnetwork.Status{
			UPSStatus:             "OL",
			UPSLoadPercent:        floatPointer(42),
			BatteryCharge:         floatPointer(100),
			BatteryRuntimeSeconds: intPointer(600),
			CheckedAt:             checkedAt,
		}},
		filePath,
		time.Hour,
		7*24*time.Hour,
		Spec{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collector.Read(context.Background()); err != nil {
		t.Fatalf("Read: %v", err)
	}
	report := collector.Report(checkedAt.Add(-time.Minute), 100)
	if !report.Connected || len(report.Points) != 1 ||
		report.Points[0].LoadPercent == nil ||
		*report.Points[0].LoadPercent != 42 {
		t.Fatalf("unexpected report: %#v", report)
	}

	reloaded, err := NewCollector(
		readerStub{},
		filePath,
		time.Hour,
		7*24*time.Hour,
		Spec{},
	)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	reloadedReport := reloaded.Report(checkedAt.Add(-time.Minute), 100)
	if len(reloadedReport.Points) != 1 {
		t.Fatalf("reloaded points=%d, want 1", len(reloadedReport.Points))
	}
}
