package configfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"powercheck/internal/core"
)

func TestLoadOverridesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := []byte(`{
		"network_confirm_seconds": 45,
		"total_budget_seconds": 240,
		"emergency_reserve_seconds": 30
	}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path, core.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if config.NetworkConfirm != 45*time.Second {
		t.Fatalf("network confirmation is %s", config.NetworkConfirm)
	}
	if config.TotalBudget != 240*time.Second {
		t.Fatalf("total budget is %s", config.TotalBudget)
	}
	if config.Interval != 5*time.Second {
		t.Fatalf("omitted default interval changed to %s", config.Interval)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"typo_seconds": 10}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, core.DefaultConfig()); err == nil {
		t.Fatal("expected unknown field to fail")
	}
}

func TestLoadRejectsTrailingJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, core.DefaultConfig()); err == nil {
		t.Fatal("expected a second JSON value to fail")
	}
}
