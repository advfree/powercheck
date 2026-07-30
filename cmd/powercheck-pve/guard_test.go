package main

import (
	"context"
	"io"
	"log"
	"sort"
	"sync"
	"testing"
	"time"

	"powercheck/internal/core"
	"powercheck/internal/dryrun"
	"powercheck/internal/pveexec"
	"powercheck/internal/pvereader"
)

type guardExecutorStub struct {
	mu             sync.Mutex
	guests         []pvereader.Guest
	stopAllCalls   int
	forceStopVMIDs []int
	poweroffCalls  int
}

func (s *guardExecutorStub) Status(context.Context) (pveexec.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return pveexec.Result{
		Action:           "status",
		Guests:           append([]pvereader.Guest(nil), s.guests...),
		AllGuestsStopped: pvereader.AllGuestsStopped(s.guests),
	}, nil
}

func (s *guardExecutorStub) StopAll(context.Context) (pveexec.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopAllCalls++
	return pveexec.Result{Action: "stopall", Executed: true}, nil
}

func (s *guardExecutorStub) ForceStopGuest(_ context.Context, vmid int) (pveexec.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forceStopVMIDs = append(s.forceStopVMIDs, vmid)
	for index := range s.guests {
		if s.guests[index].VMID == vmid {
			s.guests[index].Status = "stopped"
		}
	}
	return pveexec.Result{Action: "force-stop", Executed: true}, nil
}

func (s *guardExecutorStub) PoweroffHost(context.Context) (pveexec.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.poweroffCalls++
	return pveexec.Result{Action: "host-poweroff", Executed: true, AllGuestsStopped: true}, nil
}

func TestExecuteGuardActionsPreserveGracefulThenEmergencyOrder(t *testing.T) {
	t.Parallel()
	executor := &guardExecutorStub{guests: []pvereader.Guest{
		{VMID: 100, Type: pvereader.GuestQEMU, Status: "running"},
		{VMID: 101, Type: pvereader.GuestQEMU, Status: "stopped"},
		{VMID: 200, Type: pvereader.GuestLXC, Status: "running"},
	}}
	config := dryrun.Config{GuestShutdownTimeoutSeconds: 45}
	logger := log.New(io.Discard, "", 0)

	if err := executeGuardAction(
		context.Background(),
		core.Action{Kind: core.ActionGracefulShutdown},
		executor,
		config,
		nil,
		nil,
		logger,
	); err != nil {
		t.Fatal(err)
	}
	if executor.stopAllCalls != 1 || len(executor.forceStopVMIDs) != 0 {
		t.Fatalf("unexpected graceful calls: %#v", executor)
	}

	if err := executeGuardAction(
		context.Background(),
		core.Action{Kind: core.ActionEmergencyStopRemaining},
		executor,
		config,
		nil,
		nil,
		logger,
	); err != nil {
		t.Fatal(err)
	}
	sort.Ints(executor.forceStopVMIDs)
	if len(executor.forceStopVMIDs) != 2 ||
		executor.forceStopVMIDs[0] != 100 ||
		executor.forceStopVMIDs[1] != 200 {
		t.Fatalf("unexpected emergency VMIDs: %#v", executor.forceStopVMIDs)
	}

	if err := executeGuardAction(
		context.Background(),
		core.Action{Kind: core.ActionHostPoweroffRequested},
		executor,
		config,
		nil,
		nil,
		logger,
	); err != nil {
		t.Fatal(err)
	}
	if executor.poweroffCalls != 1 {
		t.Fatalf("poweroff calls=%d", executor.poweroffCalls)
	}
}

func TestExecuteGuardActionRejectsExpiredBudget(t *testing.T) {
	t.Parallel()
	executor := &guardExecutorStub{}
	expired := time.Now().Add(-time.Second)
	err := executeGuardAction(
		context.Background(),
		core.Action{Kind: core.ActionHostPoweroffRequested},
		executor,
		dryrun.Config{},
		nil,
		&expired,
		log.New(io.Discard, "", 0),
	)
	if err == nil {
		t.Fatal("expired total budget was accepted")
	}
	if executor.poweroffCalls != 0 {
		t.Fatalf("poweroff calls=%d", executor.poweroffCalls)
	}
}

func TestSplitTargets(t *testing.T) {
	t.Parallel()
	got := splitTargets("192.168.1.1, 192.168.1.200 ,,")
	if len(got) != 2 || got[0] != "192.168.1.1" || got[1] != "192.168.1.200" {
		t.Fatalf("unexpected targets: %#v", got)
	}
}

func TestGuardTimelineMatchesThirtyPlusFortyFiveSeconds(t *testing.T) {
	t.Parallel()
	engine, err := core.NewEngine(core.Config{
		Interval:             5 * time.Second,
		NUTConfirm:           30 * time.Second,
		NetworkConfirm:       30 * time.Second,
		TotalBudget:          120 * time.Second,
		EmergencyReserve:     45 * time.Second,
		RecoverySuccessCount: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := core.Snapshot{NUT: core.NUTOnBattery, LANReachable: true, WANReachable: true}
	var actionTimes = make(map[core.ActionKind]time.Duration)
	for at := time.Duration(0); at <= 75*time.Second; at += 5 * time.Second {
		actions, stepErr := engine.Step(at, snapshot)
		if stepErr != nil {
			t.Fatal(stepErr)
		}
		for _, action := range actions {
			actionTimes[action.Kind] = action.At
		}
	}
	if actionTimes[core.ActionGracefulShutdown] != 30*time.Second {
		t.Fatalf("graceful action at %s", actionTimes[core.ActionGracefulShutdown])
	}
	if actionTimes[core.ActionEmergencyStopRemaining] != 75*time.Second {
		t.Fatalf("emergency action at %s", actionTimes[core.ActionEmergencyStopRemaining])
	}
}
