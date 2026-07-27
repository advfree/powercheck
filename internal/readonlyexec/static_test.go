package readonlyexec

import (
	"context"
	"testing"
)

func TestStaticRunnerStillEnforcesAllowlist(t *testing.T) {
	runner := StaticRunner{GOOS: "linux"}
	if _, err := runner.Run(context.Background(), "systemctl", "poweroff"); err == nil {
		t.Fatal("static runner allowed a destructive command")
	}
}
