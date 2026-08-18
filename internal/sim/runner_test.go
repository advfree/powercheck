package sim

import (
	"os"
	"path/filepath"
	"testing"

	"powercheck/internal/core"
)

func TestAllScenarios(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "scenarios", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no scenarios found")
	}

	cfg := core.DefaultConfig()
	for _, file := range files {
		file := file
		t.Run(filepath.Base(file), func(t *testing.T) {
			scenario, err := LoadScenario(file)
			if err != nil {
				t.Fatal(err)
			}
			result, err := RunScenario(scenario, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Passed {
				t.Fatalf("scenario failed:\n%s", result.Failure)
			}
		})
	}
}

func TestInvalidScenarioIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.json")
	data := []byte(`{
		"name": "invalid",
		"duration_seconds": 10,
		"changes": [{"at_seconds": 0, "nut": "OL"}]
	}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadScenario(path); err == nil {
		t.Fatal("expected missing initial snapshot fields to be rejected")
	}
}

func TestUnknownScenarioFieldIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.json")
	data := []byte(`{
		"name": "invalid",
		"duration_seconds": 10,
		"unknown_option": true,
		"changes": [{
			"at_seconds": 0,
			"nut": "OL",
			"lan_reachable": true,
			"wan_reachable": true,
			"all_guests_stopped": true
		}],
		"expected_actions": []
	}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadScenario(path); err == nil {
		t.Fatal("expected unknown scenario field to be rejected")
	}
}
