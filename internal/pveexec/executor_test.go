package pveexec

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"powercheck/internal/pvereader"
	"powercheck/internal/readonlyexec"
)

type mutablePVE struct {
	guests   []pvereader.Guest
	commands [][]string
	fail     bool
}

func (p *mutablePVE) ListGuests(context.Context) ([]pvereader.Guest, error) {
	return append([]pvereader.Guest(nil), p.guests...), nil
}

func (p *mutablePVE) Run(_ context.Context, name string, args ...string) (readonlyexec.Output, error) {
	p.commands = append(p.commands, append([]string{name}, args...))
	if p.fail {
		return readonlyexec.Output{}, errors.New("simulated command failure")
	}
	switch {
	case name == "pvenode":
		for index := range p.guests {
			if !p.guests[index].Template {
				p.guests[index].Status = "stopped"
			}
		}
	case name == "qm" || name == "pct":
		for index := range p.guests {
			if p.guests[index].VMID == mustID(args[1]) {
				p.guests[index].Status = "stopped"
			}
		}
	}
	return readonlyexec.Output{}, nil
}

func TestGracefulGuestShutdownUsesGuestTypeAndVerifiesStatus(t *testing.T) {
	pve := &mutablePVE{guests: []pvereader.Guest{
		{VMID: 100, Name: "windows", Type: pvereader.GuestQEMU, Status: "running", Node: "pve"},
		{VMID: 200, Name: "dns", Type: pvereader.GuestLXC, Status: "running", Node: "pve"},
	}}
	executor := testExecutor(pve)

	result, err := executor.ShutdownGuest(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"qm", "shutdown", "100", "--timeout", "180"}
	if !reflect.DeepEqual(result.Command, want) || result.Guest.Status != "stopped" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.AllGuestsStopped {
		t.Fatal("second running guest was ignored")
	}

	result, err = executor.ShutdownGuest(context.Background(), 200)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"pct", "shutdown", "200", "--timeout", "180"}
	if !reflect.DeepEqual(result.Command, want) || !result.AllGuestsStopped {
		t.Fatalf("unexpected LXC result: %#v", result)
	}
}

func TestStopAllNeverEnablesPVEForceStop(t *testing.T) {
	pve := &mutablePVE{guests: []pvereader.Guest{
		{VMID: 100, Type: pvereader.GuestQEMU, Status: "running", Node: "pve"},
	}}
	result, err := testExecutor(pve).StopAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pvenode", "stopall", "--force-stop", "0", "--timeout", "180"}
	if !reflect.DeepEqual(result.Command, want) || !result.AllGuestsStopped {
		t.Fatalf("unexpected stopall result: %#v", result)
	}
}

func TestHostPoweroffIsBlockedWhileAnyGuestRuns(t *testing.T) {
	pve := &mutablePVE{guests: []pvereader.Guest{
		{VMID: 100, Type: pvereader.GuestQEMU, Status: "running", Node: "pve"},
	}}
	result, err := testExecutor(pve).PoweroffHost(context.Background())
	if err == nil {
		t.Fatal("host poweroff was not blocked")
	}
	if result.Executed || len(pve.commands) != 0 {
		t.Fatalf("poweroff command was executed: %#v", pve.commands)
	}

	pve.guests[0].Status = "stopped"
	result, err = testExecutor(pve).PoweroffHost(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Executed ||
		!reflect.DeepEqual(pve.commands, [][]string{{"systemctl", "poweroff"}}) {
		t.Fatalf("unexpected successful poweroff: %#v %#v", result, pve.commands)
	}
}

func TestCommandFailureDoesNotClaimExecution(t *testing.T) {
	pve := &mutablePVE{
		guests: []pvereader.Guest{
			{VMID: 100, Type: pvereader.GuestQEMU, Status: "running", Node: "pve"},
		},
		fail: true,
	}
	result, err := testExecutor(pve).ShutdownGuest(context.Background(), 100)
	if err == nil || result.Executed {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestWrongLocalNodeBlocksEveryWriteCommand(t *testing.T) {
	pve := &mutablePVE{guests: []pvereader.Guest{
		{VMID: 100, Type: pvereader.GuestQEMU, Status: "stopped", Node: "pve"},
	}}
	executor := testExecutor(pve)
	executor.LocalNode = "other-node"

	if _, err := executor.StopAll(context.Background()); err == nil {
		t.Fatal("stopall accepted the wrong local node")
	}
	if _, err := executor.PoweroffHost(context.Background()); err == nil {
		t.Fatal("host poweroff accepted the wrong local node")
	}
	if len(pve.commands) != 0 {
		t.Fatalf("commands ran for the wrong node: %#v", pve.commands)
	}
}

func testExecutor(pve *mutablePVE) Executor {
	return Executor{
		Runner:          pve,
		Guests:          pve,
		Node:            "pve",
		LocalNode:       "pve.example.test",
		ShutdownTimeout: 180 * time.Second,
	}
}

func mustID(raw string) int {
	var value int
	for _, character := range raw {
		value = value*10 + int(character-'0')
	}
	return value
}
