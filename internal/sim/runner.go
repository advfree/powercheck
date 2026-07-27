package sim

import (
	"fmt"
	"time"

	"powercheck/internal/core"
	"powercheck/internal/fakepve"
)

type Result struct {
	Scenario  string
	Actions   []core.Action
	PVEEvents []fakepve.Event
	Passed    bool
	Failure   string
}

func RunScenario(scenario Scenario, config core.Config) (Result, error) {
	engine, err := core.NewEngine(config)
	if err != nil {
		return Result{}, err
	}

	var pve *fakepve.Executor
	if scenario.FakePVE != nil {
		pve, err = fakepve.New(*scenario.FakePVE)
		if err != nil {
			return Result{}, err
		}
	}

	current := core.Snapshot{}
	changeIndex := 0
	restartIndex := 0
	agentTestIndex := 0
	var actions []core.Action

	for at := time.Duration(0); at <= time.Duration(scenario.DurationSeconds)*time.Second; at += config.Interval {
		seconds := int64(at / time.Second)
		for changeIndex < len(scenario.Changes) && scenario.Changes[changeIndex].AtSeconds <= seconds {
			applyChange(&current, scenario.Changes[changeIndex])
			changeIndex++
		}

		for restartIndex < len(scenario.RestartAtSeconds) && scenario.RestartAtSeconds[restartIndex] <= seconds {
			status := engine.Status()
			lastAt := engine.LastAt()
			engine, err = core.RestoreEngine(config, status, lastAt)
			if err != nil {
				return Result{}, err
			}
			restartIndex++
		}

		if pve != nil {
			pve.Advance(at)
			for agentTestIndex < len(scenario.AgentTests) &&
				scenario.AgentTests[agentTestIndex].AtSeconds <= seconds {
				if _, err := pve.TestAgent(at, scenario.AgentTests[agentTestIndex].GuestID); err != nil {
					return Result{}, fmt.Errorf("scenario %q at %s: %w", scenario.Name, at, err)
				}
				agentTestIndex++
			}
			current.AllGuestsStopped = pve.AllGuestsStopped()
		}
		stepActions, err := engine.Step(at, current)
		if err != nil {
			return Result{}, fmt.Errorf("scenario %q at %s: %w", scenario.Name, at, err)
		}
		actions = append(actions, stepActions...)
		if pve != nil {
			dispatchPVEActions(pve, stepActions)
		}
	}

	result := Result{Scenario: scenario.Name, Actions: actions}
	if pve != nil {
		result.PVEEvents = pve.Events()
	}
	result.Passed, result.Failure = compareActions(actions, scenario.ExpectedActions)
	if result.Passed && pve != nil {
		result.Passed, result.Failure = comparePVEEvents(result.PVEEvents, scenario.ExpectedPVEEvents)
	}
	return result, nil
}

func dispatchPVEActions(pve *fakepve.Executor, actions []core.Action) {
	for _, action := range actions {
		switch action.Kind {
		case core.ActionGracefulShutdown:
			pve.StartGracefulShutdown(action.At)
		case core.ActionEmergencyStopRemaining:
			pve.ForceRemaining(action.At)
		case core.ActionHostPoweroffRequested:
			pve.RequestHostPoweroff(action.At)
		}
	}
}

func applyChange(snapshot *core.Snapshot, change Change) {
	if change.NUT != nil {
		snapshot.NUT = *change.NUT
	}
	if change.LANReachable != nil {
		snapshot.LANReachable = *change.LANReachable
	}
	if change.WANReachable != nil {
		snapshot.WANReachable = *change.WANReachable
	}
	if change.AllGuestsStopped != nil {
		snapshot.AllGuestsStopped = *change.AllGuestsStopped
	}
}

func compareActions(actual []core.Action, expected []ExpectedAction) (bool, string) {
	if len(actual) != len(expected) {
		return false, fmt.Sprintf("expected %d actions, got %d", len(expected), len(actual))
	}
	for index := range expected {
		actualSeconds := int64(actual[index].At / time.Second)
		if actualSeconds != expected[index].AtSeconds || actual[index].Kind != expected[index].Kind {
			return false, fmt.Sprintf(
				"action %d: expected T+%ds %s, got T+%ds %s",
				index,
				expected[index].AtSeconds,
				expected[index].Kind,
				actualSeconds,
				actual[index].Kind,
			)
		}
	}
	return true, ""
}

func comparePVEEvents(actual []fakepve.Event, expected []ExpectedPVEEvent) (bool, string) {
	if len(actual) != len(expected) {
		return false, fmt.Sprintf("expected %d PVE events, got %d", len(expected), len(actual))
	}
	for index := range expected {
		got := actual[index]
		want := expected[index]
		actualSeconds := int64(got.At / time.Second)
		if actualSeconds != want.AtSeconds ||
			got.Kind != want.Kind ||
			got.GuestID != want.GuestID ||
			(want.Method != "" && got.Method != want.Method) ||
			(want.Command != "" && got.Command != want.Command) ||
			(want.Outcome != "" && got.Outcome != want.Outcome) {
			return false, fmt.Sprintf(
				"PVE event %d: expected T+%ds %s guest=%d method=%q outcome=%q, got T+%ds %s guest=%d method=%q outcome=%q",
				index,
				want.AtSeconds,
				want.Kind,
				want.GuestID,
				want.Method,
				want.Outcome,
				actualSeconds,
				got.Kind,
				got.GuestID,
				got.Method,
				got.Outcome,
			)
		}
	}
	return true, ""
}
