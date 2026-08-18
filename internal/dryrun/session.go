package dryrun

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"powercheck/internal/core"
	"powercheck/internal/pvereader"
)

type SnapshotCollector interface {
	Collect(context.Context) Report
}

type PlannedAction struct {
	AtSeconds int64           `json:"at_seconds"`
	Kind      core.ActionKind `json:"kind"`
	Commands  []string        `json:"would_run"`
}

type Session struct {
	engine                      *core.Engine
	collector                   SnapshotCollector
	guestShutdownTimeoutSeconds int64
}

func NewSession(config Config, collector SnapshotCollector) (*Session, error) {
	if collector == nil {
		return nil, fmt.Errorf("snapshot collector is required")
	}
	engine, err := core.NewEngine(config.Detection)
	if err != nil {
		return nil, err
	}
	return &Session{
		engine:                      engine,
		collector:                   collector,
		guestShutdownTimeoutSeconds: config.GuestShutdownTimeoutSeconds,
	}, nil
}

func (s *Session) Sample(ctx context.Context, at time.Duration) (Report, error) {
	report := s.collector.Collect(ctx)
	actions, err := s.engine.Step(at, report.Snapshot)
	if err != nil {
		return Report{}, err
	}
	for _, action := range actions {
		report.PlannedActions = append(
			report.PlannedActions,
			s.plan(action, report.Guests),
		)
	}
	return report, nil
}

func (s *Session) plan(action core.Action, guests []pvereader.Guest) PlannedAction {
	planned := PlannedAction{
		AtSeconds: int64(action.At / time.Second),
		Kind:      action.Kind,
	}
	switch action.Kind {
	case core.ActionGracefulShutdown:
		planned.Commands = []string{fmt.Sprintf(
			"pvenode stopall --force-stop 0 --timeout %d",
			s.guestShutdownTimeoutSeconds,
		)}
	case core.ActionEmergencyStopRemaining:
		for _, guest := range guests {
			if guest.Template || guest.Status == "stopped" {
				continue
			}
			command := "qm stop "
			if guest.Type == pvereader.GuestLXC {
				command = "pct stop "
			}
			planned.Commands = append(planned.Commands, command+strconv.Itoa(guest.VMID))
		}
	case core.ActionHostPoweroffRequested:
		planned.Commands = []string{"systemctl poweroff"}
	}
	return planned
}
