package core

import (
	"fmt"
	"time"
)

// Engine is a pure state machine. It never executes host commands.
type Engine struct {
	config Config
	status Status
	lastAt *time.Duration
}

func NewEngine(config Config) (*Engine, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Engine{
		config: config,
		status: Status{State: StateNormal},
	}, nil
}

func RestoreEngine(config Config, status Status, lastAt *time.Duration) (*Engine, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if status.State == "" {
		return nil, fmt.Errorf("persisted state is empty")
	}
	return &Engine{config: config, status: cloneStatus(status), lastAt: cloneDuration(lastAt)}, nil
}

func (e *Engine) Status() Status {
	return cloneStatus(e.status)
}

func (e *Engine) LastAt() *time.Duration {
	return cloneDuration(e.lastAt)
}

func (e *Engine) Step(at time.Duration, snapshot Snapshot) ([]Action, error) {
	if at < 0 {
		return nil, fmt.Errorf("sample time cannot be negative")
	}
	if e.lastAt != nil && at <= *e.lastAt {
		return nil, fmt.Errorf("sample time %s must be later than %s", at, *e.lastAt)
	}
	e.lastAt = durationPtr(at)

	if e.status.State == StatePoweredOff {
		return nil, nil
	}
	if e.status.State == StateShuttingDown {
		return e.progressShutdown(at, snapshot), nil
	}

	nutEvidence := snapshot.NUT.IsPowerLoss()
	networkEvidence := snapshot.NUT == NUTUnreachable && !snapshot.LANReachable && !snapshot.WANReachable

	var actions []Action
	if e.status.OutageStartedAt == nil && (nutEvidence || networkEvidence) {
		e.status.OutageStartedAt = durationPtr(at)
		e.status.State = StateConfirming
		actions = append(actions, Action{
			At:     at,
			Kind:   ActionOutageCandidateStarted,
			Reason: evidenceReason(nutEvidence),
		})
	}

	switch {
	case nutEvidence:
		if e.status.NUTEvidenceStartedAt == nil {
			e.status.NUTEvidenceStartedAt = durationPtr(at)
		}
		e.status.NetworkEvidenceStartedAt = nil
		e.status.RecoveryCount = 0
	case networkEvidence:
		if e.status.NetworkEvidenceStartedAt == nil {
			e.status.NetworkEvidenceStartedAt = durationPtr(at)
		}
		// Keep a preceding OB/LB timer alive if communication disappears.
		e.status.RecoveryCount = 0
	default:
		e.status.NUTEvidenceStartedAt = nil
		e.status.NetworkEvidenceStartedAt = nil
		if e.status.OutageStartedAt != nil {
			e.status.RecoveryCount++
			if e.status.RecoveryCount >= e.config.RecoverySuccessCount {
				actions = append(actions, Action{
					At:     at,
					Kind:   ActionOutageCancelled,
					Reason: "recovery confirmed by consecutive healthy samples",
				})
				e.resetCandidate()
				return actions, nil
			}
		}
	}

	if e.status.OutageStartedAt == nil {
		e.status.State = StateNormal
		return actions, nil
	}

	nutConfirmed := e.status.NUTEvidenceStartedAt != nil &&
		(nutEvidence || networkEvidence) &&
		at-*e.status.NUTEvidenceStartedAt >= e.config.NUTConfirm
	networkConfirmed := e.status.NetworkEvidenceStartedAt != nil &&
		networkEvidence &&
		at-*e.status.NetworkEvidenceStartedAt >= e.config.NetworkConfirm

	if nutConfirmed || networkConfirmed {
		e.status.State = StateShuttingDown
		e.status.GracefulIssued = true
		reason := "network inference remained active"
		if nutConfirmed {
			reason = "NUT OB/LB remained active"
		}
		actions = append(actions, Action{
			At:     at,
			Kind:   ActionGracefulShutdown,
			Reason: reason,
		})
		actions = append(actions, e.progressShutdown(at, snapshot)...)
	}

	return actions, nil
}

func (e *Engine) progressShutdown(at time.Duration, snapshot Snapshot) []Action {
	if e.status.PoweroffIssued {
		return nil
	}
	if snapshot.AllGuestsStopped {
		e.status.PoweroffIssued = true
		e.status.State = StatePoweredOff
		return []Action{{
			At:     at,
			Kind:   ActionHostPoweroffRequested,
			Reason: "all guests stopped gracefully",
		}}
	}

	if e.status.OutageStartedAt == nil {
		return nil
	}
	emergencyAt := *e.status.OutageStartedAt + e.config.TotalBudget - e.config.EmergencyReserve
	if at < emergencyAt {
		return nil
	}

	var actions []Action
	if !e.status.EmergencyIssued {
		e.status.EmergencyIssued = true
		actions = append(actions, Action{
			At:     at,
			Kind:   ActionEmergencyStopRemaining,
			Reason: "emergency reserve reached",
		})
	}
	e.status.PoweroffIssued = true
	e.status.State = StatePoweredOff
	actions = append(actions, Action{
		At:     at,
		Kind:   ActionHostPoweroffRequested,
		Reason: "request normal host poweroff after emergency guest stop",
	})
	return actions
}

func (e *Engine) resetCandidate() {
	e.status.State = StateNormal
	e.status.OutageStartedAt = nil
	e.status.NUTEvidenceStartedAt = nil
	e.status.NetworkEvidenceStartedAt = nil
	e.status.RecoveryCount = 0
}

func evidenceReason(nutEvidence bool) string {
	if nutEvidence {
		return "NUT reported OB/LB"
	}
	return "NUT, LAN and WAN became unreachable"
}

func cloneStatus(in Status) Status {
	out := in
	out.OutageStartedAt = cloneDuration(in.OutageStartedAt)
	out.NUTEvidenceStartedAt = cloneDuration(in.NUTEvidenceStartedAt)
	out.NetworkEvidenceStartedAt = cloneDuration(in.NetworkEvidenceStartedAt)
	return out
}

func cloneDuration(in *time.Duration) *time.Duration {
	if in == nil {
		return nil
	}
	return durationPtr(*in)
}

func durationPtr(value time.Duration) *time.Duration {
	copy := value
	return &copy
}
