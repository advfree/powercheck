package fakepve

import "time"

type GuestKind string

const (
	GuestVM  GuestKind = "vm"
	GuestLXC GuestKind = "lxc"
)

type AgentProbeResult string

const (
	AgentSuccess     AgentProbeResult = "success"
	AgentFailure     AgentProbeResult = "failure"
	AgentTimeout     AgentProbeResult = "timeout"
	AgentDisabled    AgentProbeResult = "disabled"
	AgentNotRelevant AgentProbeResult = "not_relevant"
)

type GracefulResult string

const (
	GracefulStops   GracefulResult = "stopped"
	GracefulStuck   GracefulResult = "stuck"
	GracefulFailure GracefulResult = "failure"
)

type ForceResult string

const (
	ForceStops   ForceResult = "stopped"
	ForceFailure ForceResult = "failure"
)

type CommandResult string

const (
	ResultSuccess CommandResult = "success"
	ResultFailure CommandResult = "failure"
	ResultBlocked CommandResult = "blocked"
)

type GuestState string

const (
	StateRunning  GuestState = "running"
	StateStopping GuestState = "stopping"
	StateStopped  GuestState = "stopped"
)

type Guest struct {
	ID                   int              `json:"id"`
	Name                 string           `json:"name"`
	Kind                 GuestKind        `json:"kind"`
	ShutdownOrder        int              `json:"shutdown_order"`
	AgentEnabled         bool             `json:"agent_enabled,omitempty"`
	AgentProbe           AgentProbeResult `json:"agent_probe,omitempty"`
	GracefulResult       GracefulResult   `json:"graceful_result,omitempty"`
	GracefulDelaySeconds int64            `json:"graceful_delay_seconds,omitempty"`
	EmergencyForceResult ForceResult      `json:"emergency_force_result,omitempty"`
}

type Config struct {
	StopAllResult         CommandResult `json:"stop_all_result,omitempty"`
	StopAllTimeoutSeconds int64         `json:"stop_all_timeout_seconds,omitempty"`
	HostPoweroffResult    CommandResult `json:"host_poweroff_result,omitempty"`
	Guests                []Guest       `json:"guests"`
}

type EventKind string

const (
	EventAgentTest              EventKind = "agent_test"
	EventPVENodeStopAll         EventKind = "pvenode_stopall"
	EventGuestShutdownRequested EventKind = "guest_shutdown_requested"
	EventGuestStopped           EventKind = "guest_stopped"
	EventGuestEmergencyForce    EventKind = "guest_emergency_force"
	EventHostPoweroff           EventKind = "host_poweroff"
)

type Event struct {
	At      time.Duration
	Kind    EventKind
	GuestID int
	Guest   string
	Method  string
	Command string
	Outcome string
	Detail  string
}

type GuestStatus struct {
	ID            int
	Name          string
	Kind          GuestKind
	State         GuestState
	LastAgentTest *AgentTestStatus
}

type AgentTestStatus struct {
	At     time.Duration
	Result AgentProbeResult
}
