package wol

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigAppliesDefaultsAndNormalizesMAC(t *testing.T) {
	t.Parallel()
	filePath := filepath.Join(t.TempDir(), "wol.json")
	content := `{
		"devices": [{
			"id": "pve-one",
			"name": "PVE One",
			"ip": "192.168.1.66",
			"mac": "7c:c3:85:be:65:cc",
			"broadcast": "192.168.1.255",
			"enabled": true
		}]
	}`
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(filePath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if config.DurationSeconds != 120 || config.IntervalSeconds != 30 {
		t.Fatalf("unexpected defaults: %#v", config)
	}
	if config.Devices[0].MAC != "7C:C3:85:BE:65:CC" || config.Devices[0].Port != 9 {
		t.Fatalf("unexpected normalized device: %#v", config.Devices[0])
	}
}

func TestConfigRejectsUnsafeOrDuplicateDevices(t *testing.T) {
	t.Parallel()
	config := Config{
		DurationSeconds: 120,
		IntervalSeconds: 30,
		Devices: []Device{
			{
				ID:        "duplicate",
				Name:      "First",
				IP:        "192.168.1.66",
				MAC:       "7C:C3:85:BE:65:CC",
				Broadcast: "192.168.1.255",
				Enabled:   true,
			},
			{
				ID:        "duplicate",
				Name:      "Second",
				IP:        "192.168.1.170",
				MAC:       "04:7C:16:0C:FC:A4",
				Broadcast: "192.168.1.255",
				Enabled:   true,
			},
		},
	}
	if err := config.Validate(); err == nil {
		t.Fatal("expected duplicate device ids to fail")
	}
	config.Devices[1].ID = "../unsafe"
	if err := config.Validate(); err == nil {
		t.Fatal("expected unsafe device id to fail")
	}
}
