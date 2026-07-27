package readonlyexec

import "testing"

func TestReadOnlyAllowlistAcceptsExpectedCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		goos string
	}{
		{"pvesh", []string{"get", "/cluster/resources", "--type", "vm", "--output-format", "json"}, "linux"},
		{"qm", []string{"agent", "100", "ping"}, "linux"},
		{"upsc", []string{"ups@synology.local:3493"}, "linux"},
		{"ping", []string{"-c", "1", "-W", "2", "192.168.1.1"}, "linux"},
		{"ping", []string{"-n", "1", "-w", "2000", "1.1.1.1"}, "windows"},
	}
	for _, test := range tests {
		if err := Validate(test.name, test.args, test.goos); err != nil {
			t.Fatalf("%s %v was rejected: %v", test.name, test.args, err)
		}
	}
}

func TestReadOnlyAllowlistRejectsDestructiveCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"qm", []string{"stop", "100"}},
		{"pct", []string{"stop", "200"}},
		{"pvenode", []string{"stopall"}},
		{"systemctl", []string{"poweroff"}},
		{"upscmd", []string{"ups@nas", "test.battery.start.quick"}},
		{"sh", []string{"-c", "echo unsafe"}},
	}
	for _, test := range tests {
		if err := Validate(test.name, test.args, "linux"); err == nil {
			t.Fatalf("%s %v was unexpectedly allowed", test.name, test.args)
		}
	}
}

func TestReadOnlyAllowlistRejectsArgumentInjection(t *testing.T) {
	tests := []struct {
		name string
		args []string
		goos string
	}{
		{"qm", []string{"agent", "100;poweroff", "ping"}, "linux"},
		{"upsc", []string{"ups@host;poweroff"}, "linux"},
		{"ping", []string{"-c", "1", "-W", "2", "-Ieth0"}, "linux"},
		{"pvesh", []string{"create", "/nodes/pve/status"}, "linux"},
	}
	for _, test := range tests {
		if err := Validate(test.name, test.args, test.goos); err == nil {
			t.Fatalf("%s %v was unexpectedly allowed", test.name, test.args)
		}
	}
}
