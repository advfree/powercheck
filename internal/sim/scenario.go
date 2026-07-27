package sim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"powercheck/internal/core"
	"powercheck/internal/fakepve"
)

type Scenario struct {
	Name              string             `json:"name"`
	Description       string             `json:"description"`
	DurationSeconds   int64              `json:"duration_seconds"`
	Changes           []Change           `json:"changes"`
	RestartAtSeconds  []int64            `json:"restart_at_seconds,omitempty"`
	ExpectedActions   []ExpectedAction   `json:"expected_actions"`
	FakePVE           *fakepve.Config    `json:"fake_pve,omitempty"`
	AgentTests        []AgentTest        `json:"agent_tests,omitempty"`
	ExpectedPVEEvents []ExpectedPVEEvent `json:"expected_pve_events,omitempty"`
}

type Change struct {
	AtSeconds        int64           `json:"at_seconds"`
	NUT              *core.NUTStatus `json:"nut,omitempty"`
	LANReachable     *bool           `json:"lan_reachable,omitempty"`
	WANReachable     *bool           `json:"wan_reachable,omitempty"`
	AllGuestsStopped *bool           `json:"all_guests_stopped,omitempty"`
}

type ExpectedAction struct {
	AtSeconds int64           `json:"at_seconds"`
	Kind      core.ActionKind `json:"kind"`
}

type ExpectedPVEEvent struct {
	AtSeconds int64             `json:"at_seconds"`
	Kind      fakepve.EventKind `json:"kind"`
	GuestID   int               `json:"guest_id,omitempty"`
	Method    string            `json:"method,omitempty"`
	Command   string            `json:"command,omitempty"`
	Outcome   string            `json:"outcome,omitempty"`
}

type AgentTest struct {
	AtSeconds int64 `json:"at_seconds"`
	GuestID   int   `json:"guest_id"`
}

func LoadScenario(path string) (Scenario, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Scenario{}, err
	}

	var scenario Scenario
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&scenario); err != nil {
		return Scenario{}, fmt.Errorf("decode %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values are not allowed")
		}
		return Scenario{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if err := scenario.Validate(); err != nil {
		return Scenario{}, fmt.Errorf("validate %s: %w", path, err)
	}
	return scenario, nil
}

func (s *Scenario) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("name is required")
	}
	if s.DurationSeconds < 0 {
		return fmt.Errorf("duration cannot be negative")
	}
	if len(s.Changes) == 0 || s.Changes[0].AtSeconds != 0 {
		return fmt.Errorf("the first change must start at 0 seconds")
	}
	first := s.Changes[0]
	if first.NUT == nil || first.LANReachable == nil || first.WANReachable == nil {
		return fmt.Errorf("the first change must provide NUT, LAN and WAN state")
	}
	if s.FakePVE == nil && first.AllGuestsStopped == nil {
		return fmt.Errorf("the first change must provide a complete snapshot")
	}
	if s.FakePVE != nil {
		executor, err := fakepve.New(*s.FakePVE)
		if err != nil {
			return fmt.Errorf("fake PVE: %w", err)
		}
		for _, change := range s.Changes {
			if change.AllGuestsStopped != nil {
				return fmt.Errorf("all_guests_stopped cannot be set when fake_pve is enabled")
			}
		}
		for _, test := range s.AgentTests {
			if test.AtSeconds < 0 || test.AtSeconds > s.DurationSeconds {
				return fmt.Errorf("agent test time %d is outside the scenario", test.AtSeconds)
			}
			if _, err := executor.TestAgent(0, test.GuestID); err != nil {
				return fmt.Errorf("agent test: %w", err)
			}
		}
	} else if len(s.ExpectedPVEEvents) > 0 || len(s.AgentTests) > 0 {
		return fmt.Errorf("expected_pve_events and agent_tests require fake_pve")
	}

	sort.Slice(s.Changes, func(i, j int) bool {
		return s.Changes[i].AtSeconds < s.Changes[j].AtSeconds
	})
	sort.Slice(s.RestartAtSeconds, func(i, j int) bool {
		return s.RestartAtSeconds[i] < s.RestartAtSeconds[j]
	})
	sort.SliceStable(s.AgentTests, func(i, j int) bool {
		return s.AgentTests[i].AtSeconds < s.AgentTests[j].AtSeconds
	})

	for _, change := range s.Changes {
		if change.AtSeconds < 0 || change.AtSeconds > s.DurationSeconds {
			return fmt.Errorf("change time %d is outside the scenario", change.AtSeconds)
		}
		if change.NUT != nil && !validNUTStatus(*change.NUT) {
			return fmt.Errorf("unknown NUT status %q", *change.NUT)
		}
	}
	for _, restartAt := range s.RestartAtSeconds {
		if restartAt < 0 || restartAt > s.DurationSeconds {
			return fmt.Errorf("restart time %d is outside the scenario", restartAt)
		}
	}
	for _, expected := range s.ExpectedActions {
		if expected.AtSeconds < 0 || expected.AtSeconds > s.DurationSeconds {
			return fmt.Errorf("expected action time %d is outside the scenario", expected.AtSeconds)
		}
		if !validActionKind(expected.Kind) {
			return fmt.Errorf("unknown expected action %q", expected.Kind)
		}
	}
	for _, expected := range s.ExpectedPVEEvents {
		if expected.AtSeconds < 0 || expected.AtSeconds > s.DurationSeconds {
			return fmt.Errorf("expected PVE event time %d is outside the scenario", expected.AtSeconds)
		}
		if !validPVEEventKind(expected.Kind) {
			return fmt.Errorf("unknown expected PVE event %q", expected.Kind)
		}
	}
	return nil
}

func validNUTStatus(status core.NUTStatus) bool {
	switch status {
	case core.NUTOnline, core.NUTOnBattery, core.NUTLowBattery, core.NUTUnreachable:
		return true
	default:
		return false
	}
}

func validActionKind(kind core.ActionKind) bool {
	switch kind {
	case core.ActionOutageCandidateStarted,
		core.ActionOutageCancelled,
		core.ActionGracefulShutdown,
		core.ActionEmergencyStopRemaining,
		core.ActionHostPoweroffRequested:
		return true
	default:
		return false
	}
}

func validPVEEventKind(kind fakepve.EventKind) bool {
	switch kind {
	case fakepve.EventAgentTest,
		fakepve.EventPVENodeStopAll,
		fakepve.EventGuestShutdownRequested,
		fakepve.EventGuestStopped,
		fakepve.EventGuestEmergencyForce,
		fakepve.EventHostPoweroff:
		return true
	default:
		return false
	}
}
