package core

import "time"

type NUTStatus string

const (
	NUTOnline      NUTStatus = "OL"
	NUTOnBattery   NUTStatus = "OB"
	NUTLowBattery  NUTStatus = "LB"
	NUTUnreachable NUTStatus = "UNREACHABLE"
)

func (s NUTStatus) IsPowerLoss() bool {
	return s == NUTOnBattery || s == NUTLowBattery
}

type Snapshot struct {
	NUT              NUTStatus `json:"nut"`
	LANReachable     bool      `json:"lan_reachable"`
	WANReachable     bool      `json:"wan_reachable"`
	AllGuestsStopped bool      `json:"all_guests_stopped"`
}

type State string

const (
	StateNormal       State = "NORMAL"
	StateConfirming   State = "CONFIRMING_OUTAGE"
	StateShuttingDown State = "SHUTTING_DOWN"
	StatePoweredOff   State = "POWERED_OFF"
)

type ActionKind string

const (
	ActionOutageCandidateStarted ActionKind = "outage_candidate_started"
	ActionOutageCancelled        ActionKind = "outage_cancelled"
	ActionGracefulShutdown       ActionKind = "graceful_shutdown"
	ActionEmergencyStopRemaining ActionKind = "emergency_stop_remaining"
	ActionHostPoweroffRequested  ActionKind = "host_poweroff_requested"
)

type Action struct {
	At     time.Duration
	Kind   ActionKind
	Reason string
}

type Status struct {
	State                    State          `json:"state"`
	OutageStartedAt          *time.Duration `json:"outage_started_at,omitempty"`
	NUTEvidenceStartedAt     *time.Duration `json:"nut_evidence_started_at,omitempty"`
	NetworkEvidenceStartedAt *time.Duration `json:"network_evidence_started_at,omitempty"`
	RecoveryCount            int            `json:"recovery_count"`
	GracefulIssued           bool           `json:"graceful_issued"`
	EmergencyIssued          bool           `json:"emergency_issued"`
	PoweroffIssued           bool           `json:"poweroff_issued"`
}
