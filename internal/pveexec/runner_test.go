package pveexec

import "testing"

func TestPowerAllowlistAcceptsOnlyExpectedCommands(t *testing.T) {
	accepted := []struct {
		name string
		args []string
	}{
		{"pvenode", []string{"stopall", "--force-stop", "0", "--timeout", "180"}},
		{"qm", []string{"shutdown", "100", "--timeout", "180"}},
		{"pct", []string{"shutdown", "200", "--timeout", "60"}},
		{"qm", []string{"stop", "100"}},
		{"pct", []string{"stop", "200"}},
		{"systemctl", []string{"poweroff"}},
	}
	for _, test := range accepted {
		if err := Validate(test.name, test.args); err != nil {
			t.Fatalf("%s %v was rejected: %v", test.name, test.args, err)
		}
	}

	rejected := []struct {
		name string
		args []string
	}{
		{"pvenode", []string{"stopall"}},
		{"pvenode", []string{"stopall", "--force-stop", "1", "--timeout", "180"}},
		{"qm", []string{"stop", "100;poweroff"}},
		{"qm", []string{"shutdown", "100", "--timeout", "0"}},
		{"pct", []string{"destroy", "200"}},
		{"systemctl", []string{"reboot"}},
		{"sh", []string{"-c", "poweroff"}},
	}
	for _, test := range rejected {
		if err := Validate(test.name, test.args); err == nil {
			t.Fatalf("%s %v was unexpectedly accepted", test.name, test.args)
		}
	}
}
