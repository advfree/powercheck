package outageconfig

import (
	"testing"

	"powercheck/internal/core"
	"powercheck/internal/pvereader"
)

func TestSimulateNUTOutageReturnsWouldRunWithoutExecutor(t *testing.T) {
	config := Default()
	result, err := Simulate(config, ScenarioNUT, []pvereader.Guest{
		{VMID: 100, Type: pvereader.GuestQEMU, Status: "running"},
		{VMID: 102, Type: pvereader.GuestQEMU, Status: "running"},
		{VMID: 106, Type: pvereader.GuestQEMU, Status: "stopped"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != ModeDryRun || len(result.Steps) != 4 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Steps[1].Kind != core.ActionGracefulShutdown ||
		result.Steps[1].AtSeconds != 30 ||
		len(result.Steps[1].WouldRun) != 1 {
		t.Fatalf("unexpected graceful step: %#v", result.Steps[1])
	}
	if result.Steps[2].Kind != core.ActionEmergencyStopRemaining ||
		result.Steps[2].AtSeconds != 75 ||
		len(result.Steps[2].WouldRun) != 2 {
		t.Fatalf("unexpected emergency step: %#v", result.Steps[2])
	}
	if result.Steps[3].Kind != core.ActionHostPoweroffRequested {
		t.Fatalf("unexpected poweroff step: %#v", result.Steps[3])
	}
}

func TestSimulateUsesSampleBoundary(t *testing.T) {
	config := Default()
	config.IntervalSeconds = 7
	result, err := Simulate(config, ScenarioNUT, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Steps[1].AtSeconds != 35 {
		t.Fatalf("graceful step at %d, want 35", result.Steps[1].AtSeconds)
	}
}

func TestSimulateSkipsEmergencyWhenAllGuestsAlreadyStopped(t *testing.T) {
	config := Default()
	result, err := Simulate(config, ScenarioNUT, []pvereader.Guest{
		{VMID: 100, Type: pvereader.GuestQEMU, Status: "stopped"},
		{VMID: 102, Type: pvereader.GuestQEMU, Status: "stopped"},
		{VMID: 103, Type: pvereader.GuestQEMU, Status: "stopped"},
		{VMID: 106, Type: pvereader.GuestQEMU, Status: "stopped"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps) != 3 {
		t.Fatalf("unexpected steps: %#v", result.Steps)
	}
	if result.Steps[1].Kind != core.ActionGracefulShutdown ||
		result.Steps[1].AtSeconds != 30 {
		t.Fatalf("unexpected graceful step: %#v", result.Steps[1])
	}
	if result.Steps[2].Kind != core.ActionHostPoweroffRequested ||
		result.Steps[2].AtSeconds != 30 {
		t.Fatalf("unexpected poweroff step: %#v", result.Steps[2])
	}
	for _, step := range result.Steps {
		if step.Kind == core.ActionEmergencyStopRemaining {
			t.Fatalf("unexpected emergency step: %#v", step)
		}
	}
}
