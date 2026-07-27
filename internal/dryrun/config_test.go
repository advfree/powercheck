package dryrun

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExampleConfigLoads(t *testing.T) {
	config, err := LoadConfig(filepath.Join("..", "..", "powercheck-dryrun.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	if config.Detection.Interval != 5*time.Second ||
		config.Detection.NetworkConfirm != 60*time.Second ||
		config.GuestShutdownTimeoutSeconds != 180 {
		t.Fatalf("unexpected configuration: %#v", config)
	}
}

func TestConfigRequiresExplicitDryRunMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := []byte(`{
		"mode": "armed",
		"pve_node": "pve",
		"nut_target": "ups@nas",
		"lan_targets": ["nas"],
		"wan_targets": ["1.1.1.1", "223.5.5.5"]
	}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected non-dry-run mode to be rejected")
	}
}

func TestConfigRejectsCommandInjectionTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := []byte(`{
		"mode": "dry-run",
		"pve_node": "pve",
		"nut_target": "ups@nas;poweroff",
		"lan_targets": ["nas"],
		"wan_targets": ["1.1.1.1", "223.5.5.5"]
	}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("expected unsafe NUT target to be rejected")
	}
}
