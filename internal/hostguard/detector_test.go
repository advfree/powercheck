package hostguard

import (
	"testing"
	"time"

	"powercheck/internal/core"
)

func testConfig() core.Config {
	return core.Config{
		Interval:             5 * time.Second,
		NUTConfirm:           30 * time.Second,
		NetworkConfirm:       30 * time.Second,
		TotalBudget:          120 * time.Second,
		EmergencyReserve:     45 * time.Second,
		RecoverySuccessCount: 3,
	}
}

func TestNUTOnBatteryRequestsPoweroffAfterConfirmation(t *testing.T) {
	detector, err := NewDetector(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	actions, err := detector.Step(0, Sample{
		UPSStatus:    "OB",
		NUTReachable: true,
		LANReachable: true,
		WANReachable: true,
	})
	if err != nil || RequestsPoweroff(actions) {
		t.Fatalf("initial actions=%#v err=%v", actions, err)
	}
	actions, err = detector.Step(30*time.Second, Sample{
		UPSStatus:    "OB LB",
		NUTReachable: true,
		LANReachable: true,
		WANReachable: true,
	})
	if err != nil || !RequestsPoweroff(actions) {
		t.Fatalf("confirmed actions=%#v err=%v", actions, err)
	}
}

func TestNetworkInferenceRequiresNUTLANAndWANFailure(t *testing.T) {
	detector, err := NewDetector(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	for index, sample := range []Sample{
		{NUTReachable: false, LANReachable: true, WANReachable: false},
		{NUTReachable: false, LANReachable: false, WANReachable: true},
	} {
		actions, stepErr := detector.Step(time.Duration(index)*5*time.Second, sample)
		if stepErr != nil || len(actions) != 0 {
			t.Fatalf("sample %d actions=%#v err=%v", index, actions, stepErr)
		}
	}
}

func TestHealthySamplesCancelCandidate(t *testing.T) {
	detector, err := NewDetector(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := detector.Step(0, Sample{}); err != nil {
		t.Fatal(err)
	}
	for _, at := range []time.Duration{5 * time.Second, 10 * time.Second, 15 * time.Second} {
		actions, stepErr := detector.Step(at, Sample{
			UPSStatus:    "OL",
			NUTReachable: true,
			LANReachable: true,
			WANReachable: true,
		})
		if stepErr != nil {
			t.Fatal(stepErr)
		}
		if at == 15*time.Second && len(actions) != 1 {
			t.Fatalf("recovery actions=%#v", actions)
		}
	}
}

func TestUnknownNUTStatusIsNotPowerLoss(t *testing.T) {
	if got := parseUPSStatus("CAL"); got != core.NUTUnreachable {
		t.Fatalf("status=%s, want unreachable", got)
	}
}
