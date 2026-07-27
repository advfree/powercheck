package dryrun

import (
	"context"
	"reflect"
	"testing"
	"time"

	"powercheck/internal/core"
	"powercheck/internal/nutreader"
	"powercheck/internal/pvereader"
	"powercheck/internal/reachability"
)

type sequenceCollector struct {
	reports []Report
	index   int
}

func (c *sequenceCollector) Collect(context.Context) Report {
	report := c.reports[c.index]
	if c.index < len(c.reports)-1 {
		c.index++
	}
	return report
}

func TestSessionOnlyPlansCommands(t *testing.T) {
	snapshot := core.Snapshot{
		NUT:              core.NUTUnreachable,
		LANReachable:     false,
		WANReachable:     false,
		AllGuestsStopped: false,
	}
	guests := []pvereader.Guest{
		{VMID: 100, Name: "app", Type: pvereader.GuestQEMU, Status: "running"},
		{VMID: 200, Name: "dns", Type: pvereader.GuestLXC, Status: "stopped"},
		{VMID: 300, Name: "template", Type: pvereader.GuestQEMU, Status: "running", Template: true},
	}
	collector := &sequenceCollector{reports: []Report{
		{Mode: "dry-run", Snapshot: snapshot, Guests: guests},
		{Mode: "dry-run", Snapshot: snapshot, Guests: guests},
		{Mode: "dry-run", Snapshot: snapshot, Guests: guests},
	}}
	config := validTestConfig()
	session, err := NewSession(config, collector)
	if err != nil {
		t.Fatal(err)
	}

	first, err := session.Sample(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.PlannedActions) != 1 ||
		first.PlannedActions[0].Kind != core.ActionOutageCandidateStarted ||
		len(first.PlannedActions[0].Commands) != 0 {
		t.Fatalf("unexpected initial plan: %#v", first.PlannedActions)
	}

	confirmed, err := session.Sample(context.Background(), 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	wantStopAll := []string{"pvenode stopall --force-stop 0 --timeout 180"}
	if !reflect.DeepEqual(confirmed.PlannedActions[0].Commands, wantStopAll) {
		t.Fatalf("graceful plan is %#v", confirmed.PlannedActions)
	}

	emergency, err := session.Sample(context.Background(), 240*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(emergency.PlannedActions) != 2 {
		t.Fatalf("unexpected emergency plan: %#v", emergency.PlannedActions)
	}
	if !reflect.DeepEqual(emergency.PlannedActions[0].Commands, []string{"qm stop 100"}) {
		t.Fatalf("emergency plan included stopped guests or templates: %#v", emergency.PlannedActions[0])
	}
	if !reflect.DeepEqual(emergency.PlannedActions[1].Commands, []string{"systemctl poweroff"}) {
		t.Fatalf("unexpected host plan: %#v", emergency.PlannedActions[1])
	}
}

type pveStub struct {
	guests []pvereader.Guest
}

func (s pveStub) ListGuests(context.Context) ([]pvereader.Guest, error) {
	return s.guests, nil
}

type nutStub struct {
	reading nutreader.Reading
}

func (s nutStub) Read(context.Context) (nutreader.Reading, error) {
	return s.reading, nil
}

type pingStub struct {
	reachable map[string]bool
}

func (s pingStub) Probe(_ context.Context, target string) reachability.Result {
	return reachability.Result{Target: target, Reachable: s.reachable[target]}
}

func TestCollectorBuildsOneAtomicSnapshot(t *testing.T) {
	config := validTestConfig()
	collector, err := NewCollector(
		config,
		pveStub{guests: []pvereader.Guest{
			{VMID: 100, Type: pvereader.GuestQEMU, Status: "running"},
		}},
		nutStub{reading: nutreader.Reading{Target: "ups@nas", Status: core.NUTOnline}},
		pingStub{reachable: map[string]bool{
			"nas":       true,
			"1.1.1.1":   false,
			"223.5.5.5": false,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	report := collector.Collect(context.Background())
	if report.Snapshot.NUT != core.NUTOnline ||
		!report.Snapshot.LANReachable ||
		report.Snapshot.WANReachable ||
		report.Snapshot.AllGuestsStopped {
		t.Fatalf("unexpected snapshot: %#v", report.Snapshot)
	}
}

func validTestConfig() Config {
	return Config{
		Detection:                   core.DefaultConfig(),
		RoundTimeout:                3 * time.Second,
		PingTimeout:                 2 * time.Second,
		PVECommandTimeout:           3 * time.Second,
		GuestShutdownTimeoutSeconds: 180,
		PVENode:                     "pve",
		NUTTarget:                   "ups@nas",
		LANTargets:                  []string{"nas"},
		WANTargets:                  []string{"1.1.1.1", "223.5.5.5"},
	}
}
