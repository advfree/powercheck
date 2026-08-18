package core

import (
	"testing"
	"time"
)

func TestRestoreKeepsOriginalOutageStart(t *testing.T) {
	cfg := DefaultConfig()
	start := time.Duration(0)
	snapshot := Snapshot{
		NUT:              NUTUnreachable,
		LANReachable:     false,
		WANReachable:     false,
		AllGuestsStopped: false,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Step(start, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Step(start+30*time.Second, snapshot); err != nil {
		t.Fatal(err)
	}

	restored, err := RestoreEngine(cfg, engine.Status(), engine.LastAt())
	if err != nil {
		t.Fatal(err)
	}
	actions, err := restored.Step(start+60*time.Second, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Kind != ActionGracefulShutdown {
		t.Fatalf("expected graceful shutdown at original T+60, got %#v", actions)
	}
}

func TestEmergencyUsesTotalBudgetMinusReserve(t *testing.T) {
	cfg := DefaultConfig()
	start := time.Duration(0)
	snapshot := Snapshot{NUT: NUTOnBattery}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Step(start, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Step(start+30*time.Second, snapshot); err != nil {
		t.Fatal(err)
	}
	actions, err := engine.Step(start+240*time.Second, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 2 {
		t.Fatalf("expected emergency stop and host poweroff, got %#v", actions)
	}
	if actions[0].Kind != ActionEmergencyStopRemaining || actions[1].Kind != ActionHostPoweroffRequested {
		t.Fatalf("unexpected emergency actions: %#v", actions)
	}
}

func TestInvalidConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EmergencyReserve = cfg.TotalBudget
	if _, err := NewEngine(cfg); err == nil {
		t.Fatal("expected invalid reserve to be rejected")
	}
}
