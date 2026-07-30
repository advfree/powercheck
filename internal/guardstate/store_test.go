package guardstate

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"powercheck/internal/core"
)

func TestStoreRestoresActiveStateOnSameBoot(t *testing.T) {
	directory := t.TempDir()
	bootIDPath := filepath.Join(directory, "boot-id")
	if err := os.WriteFile(bootIDPath, []byte("boot-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := Store{Path: filepath.Join(directory, "state.json"), BootIDPath: bootIDPath}
	config := core.Config{
		Interval:             5 * time.Second,
		NUTConfirm:           30 * time.Second,
		NetworkConfirm:       30 * time.Second,
		TotalBudget:          120 * time.Second,
		EmergencyReserve:     45 * time.Second,
		RecoverySuccessCount: 3,
	}
	engine, err := core.NewEngine(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Step(5*time.Second, core.Snapshot{NUT: core.NUTOnBattery}); err != nil {
		t.Fatal(err)
	}
	origin := time.Now().Add(-5 * time.Second).Round(time.Millisecond)
	if err := store.Save(origin, 7, config, engine); err != nil {
		t.Fatal(err)
	}

	restored, restoredConfig, restoredOrigin, revision, found, err := store.Restore()
	if err != nil {
		t.Fatal(err)
	}
	if !found || revision != 7 || !restoredOrigin.Equal(origin) {
		t.Fatalf("unexpected restore metadata: found=%t revision=%d origin=%s", found, revision, restoredOrigin)
	}
	if restored.Status().State != core.StateConfirming {
		t.Fatalf("unexpected restored state: %s", restored.Status().State)
	}
	if restoredConfig.TotalBudget != config.TotalBudget {
		t.Fatalf("unexpected restored config: %#v", restoredConfig)
	}
}

func TestStoreDiscardsStateFromEarlierBoot(t *testing.T) {
	directory := t.TempDir()
	bootIDPath := filepath.Join(directory, "boot-id")
	if err := os.WriteFile(bootIDPath, []byte("boot-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := Store{Path: filepath.Join(directory, "state.json"), BootIDPath: bootIDPath}
	config := core.Config{
		Interval:             time.Second,
		NUTConfirm:           5 * time.Second,
		NetworkConfirm:       5 * time.Second,
		TotalBudget:          60 * time.Second,
		EmergencyReserve:     10 * time.Second,
		RecoverySuccessCount: 1,
	}
	engine, err := core.NewEngine(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Step(time.Second, core.Snapshot{NUT: core.NUTOnBattery}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(time.Now(), 1, config, engine); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bootIDPath, []byte("boot-b\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, _, _, found, err := store.Restore()
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("state from an earlier boot must not be restored")
	}
	if _, err := os.Stat(store.Path); !os.IsNotExist(err) {
		t.Fatalf("stale state file remains: %v", err)
	}
}
