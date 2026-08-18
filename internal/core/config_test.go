package core

import (
	"testing"
	"time"
)

func TestDefaultConfigUsesSafeFirstInstallValues(t *testing.T) {
	got := DefaultConfig()

	if got.Interval != 5*time.Second {
		t.Fatalf("default interval is %s, want 5s", got.Interval)
	}
	if got.NUTConfirm != 30*time.Second {
		t.Fatalf("default NUT confirmation is %s, want 30s", got.NUTConfirm)
	}
	if got.NetworkConfirm != 60*time.Second {
		t.Fatalf("default network confirmation is %s, want 60s", got.NetworkConfirm)
	}
	if got.TotalBudget != 300*time.Second {
		t.Fatalf("default total budget is %s, want 300s", got.TotalBudget)
	}
	if got.EmergencyReserve != 60*time.Second {
		t.Fatalf("default emergency reserve is %s, want 60s", got.EmergencyReserve)
	}
	if got.RecoverySuccessCount != 3 {
		t.Fatalf("default recovery count is %d, want 3", got.RecoverySuccessCount)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("default config must be valid: %v", err)
	}
}
