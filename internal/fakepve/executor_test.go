package fakepve

import (
	"reflect"
	"testing"
	"time"
)

func TestGracefulShutdownUsesConfiguredOrderAndMethods(t *testing.T) {
	executor, err := New(Config{
		Guests: []Guest{
			{
				ID: 300, Name: "truenas", Kind: GuestVM, ShutdownOrder: 100,
				AgentEnabled: true, GracefulDelaySeconds: 60,
			},
			{
				ID: 101, Name: "windows", Kind: GuestVM, ShutdownOrder: 20,
				GracefulDelaySeconds: 40,
			},
			{
				ID: 200, Name: "dns", Kind: GuestLXC, ShutdownOrder: 30,
				GracefulDelaySeconds: 10,
			},
			{
				ID: 100, Name: "app", Kind: GuestVM, ShutdownOrder: 10,
				AgentEnabled: true, GracefulDelaySeconds: 20,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	executor.StartGracefulShutdown(30 * time.Second)
	events := executor.Events()
	if len(events) != 5 {
		t.Fatalf("expected stopall and four shutdown requests, got %#v", events)
	}
	if events[0].Kind != EventPVENodeStopAll ||
		events[0].Command != "pvenode stopall --force-stop 0 --timeout 180" {
		t.Fatalf("unexpected stopall event: %#v", events[0])
	}

	var gotIDs []int
	var gotMethods []string
	for _, event := range events[1:] {
		gotIDs = append(gotIDs, event.GuestID)
		gotMethods = append(gotMethods, event.Method)
	}
	if want := []int{100, 101, 200, 300}; !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("shutdown order is %v, want %v", gotIDs, want)
	}
	if want := []string{"qemu_guest_agent", "acpi", "lxc_shutdown", "qemu_guest_agent"}; !reflect.DeepEqual(gotMethods, want) {
		t.Fatalf("shutdown methods are %v, want %v", gotMethods, want)
	}

	executor.Advance(90 * time.Second)
	if !executor.AllGuestsStopped() {
		t.Fatalf("guests did not stop: %#v", executor.GuestStatuses())
	}
}

func TestAgentTestsAreNonDestructiveAndReportOutcomes(t *testing.T) {
	executor, err := New(Config{
		Guests: []Guest{
			{ID: 100, Name: "ok", Kind: GuestVM, AgentEnabled: true, AgentProbe: AgentSuccess},
			{ID: 101, Name: "failed", Kind: GuestVM, AgentEnabled: true, AgentProbe: AgentFailure},
			{ID: 102, Name: "timeout", Kind: GuestVM, AgentEnabled: true, AgentProbe: AgentTimeout},
			{ID: 103, Name: "disabled", Kind: GuestVM},
			{ID: 200, Name: "container", Kind: GuestLXC},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	want := map[int]AgentProbeResult{
		100: AgentSuccess,
		101: AgentFailure,
		102: AgentTimeout,
		103: AgentDisabled,
		200: AgentNotRelevant,
	}
	for id, expected := range want {
		result, err := executor.TestAgent(time.Second, id)
		if err != nil {
			t.Fatal(err)
		}
		if result != expected {
			t.Fatalf("guest %d returned %q, want %q", id, result, expected)
		}
	}
	for _, status := range executor.GuestStatuses() {
		if status.State != StateRunning {
			t.Fatalf("agent test changed guest state: %#v", status)
		}
		if status.LastAgentTest == nil || status.LastAgentTest.Result != want[status.ID] {
			t.Fatalf("agent test result was not retained: %#v", status)
		}
	}
}

func TestStopAllFailureLeavesGuestsForEmergency(t *testing.T) {
	executor, err := New(Config{
		StopAllResult: ResultFailure,
		Guests: []Guest{
			{ID: 100, Name: "app", Kind: GuestVM},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	executor.StartGracefulShutdown(30 * time.Second)
	events := executor.Events()
	if len(events) != 1 || events[0].Outcome != string(ResultFailure) {
		t.Fatalf("unexpected events: %#v", events)
	}
	if executor.AllGuestsStopped() {
		t.Fatal("failed stopall unexpectedly stopped guests")
	}

	executor.ForceRemaining(240 * time.Second)
	if !executor.AllGuestsStopped() {
		t.Fatal("emergency force did not stop the remaining guest")
	}
}

func TestFailedForceBlocksHostPoweroff(t *testing.T) {
	executor, err := New(Config{
		Guests: []Guest{
			{
				ID: 100, Name: "stuck", Kind: GuestVM,
				GracefulResult: GracefulStuck, EmergencyForceResult: ForceFailure,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	executor.StartGracefulShutdown(30 * time.Second)
	executor.ForceRemaining(240 * time.Second)
	if result := executor.RequestHostPoweroff(240 * time.Second); result != ResultBlocked {
		t.Fatalf("poweroff result is %q, want blocked", result)
	}

	events := executor.Events()
	last := events[len(events)-1]
	if last.Kind != EventHostPoweroff || last.Outcome != string(ResultBlocked) {
		t.Fatalf("unexpected final event: %#v", last)
	}
}

func TestLXCForceUsesPctAndHostFailureIsReported(t *testing.T) {
	executor, err := New(Config{
		HostPoweroffResult: ResultFailure,
		Guests: []Guest{
			{
				ID: 200, Name: "container", Kind: GuestLXC,
				GracefulResult: GracefulStuck,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	executor.StartGracefulShutdown(30 * time.Second)
	executor.ForceRemaining(240 * time.Second)
	if result := executor.RequestHostPoweroff(240 * time.Second); result != ResultFailure {
		t.Fatalf("poweroff result is %q, want failure", result)
	}

	events := executor.Events()
	force := events[len(events)-2]
	if force.Kind != EventGuestEmergencyForce || force.Command != "pct stop 200" {
		t.Fatalf("unexpected LXC force event: %#v", force)
	}
	poweroff := events[len(events)-1]
	if poweroff.Kind != EventHostPoweroff || poweroff.Outcome != string(ResultFailure) {
		t.Fatalf("unexpected host poweroff event: %#v", poweroff)
	}
}

func TestDuplicateGuestIDIsRejected(t *testing.T) {
	_, err := New(Config{Guests: []Guest{
		{ID: 100, Name: "one", Kind: GuestVM},
		{ID: 100, Name: "two", Kind: GuestVM},
	}})
	if err == nil {
		t.Fatal("expected duplicate guest ID to fail")
	}
}
