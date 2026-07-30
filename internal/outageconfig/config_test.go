package outageconfig

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestStoreReturnsDefaultsAndPersistsValidatedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "powercheck", "outage-config.json")
	store := Store{Path: path}
	config, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.Mode != ModeProduction ||
		config.IntervalSeconds != 5 ||
		config.NUTConfirmSeconds != 30 ||
		config.NetworkConfirmSeconds != 30 ||
		config.TotalBudgetSeconds != 120 ||
		config.EmergencyReserveSeconds != 45 ||
		config.RecoverySuccessCount != 3 ||
		config.GuestShutdownTimeoutSeconds != 45 ||
		config.Revision != 0 {
		t.Fatalf("unexpected defaults: %#v", config)
	}

	config.NUTConfirmSeconds = 45
	config.Revision = 1
	config.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	if err := store.Save(config); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.NUTConfirmSeconds != 45 || loaded.Revision != 1 ||
		!loaded.UpdatedAt.Equal(config.UpdatedAt) {
		t.Fatalf("unexpected persisted config: %#v", loaded)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o, want 600", info.Mode().Perm())
	}
}

func TestConfigRejectsUnknownModeAndUnsafeTiming(t *testing.T) {
	config := Default()
	config.Mode = "execute"
	if err := config.Validate(); err == nil {
		t.Fatal("unknown mode was accepted")
	}
	config = Default()
	config.TotalBudgetSeconds = config.EmergencyReserveSeconds + config.NUTConfirmSeconds
	if err := config.Validate(); err == nil {
		t.Fatal("timing without graceful window was accepted")
	}
}
