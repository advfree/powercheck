package fakepve

import (
	"fmt"
	"sort"
	"time"
)

type guestRuntime struct {
	spec          Guest
	state         GuestState
	stopDue       *time.Duration
	lastAgentTest *AgentTestStatus
}

// Executor is a pure in-memory PVE implementation. It only records commands
// that a production executor would run.
type Executor struct {
	config          Config
	guests          []*guestRuntime
	events          []Event
	gracefulStarted bool
}

func New(config Config) (*Executor, error) {
	normalized, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}

	executor := &Executor{config: normalized}
	for _, guest := range normalized.Guests {
		executor.guests = append(executor.guests, &guestRuntime{
			spec:  guest,
			state: StateRunning,
		})
	}
	executor.sortGuests()
	return executor, nil
}

func (e *Executor) Advance(at time.Duration) {
	pending := make([]*guestRuntime, 0)
	for _, guest := range e.guests {
		if guest.state == StateStopping && guest.stopDue != nil && *guest.stopDue <= at {
			pending = append(pending, guest)
		}
	}
	sort.SliceStable(pending, func(i, j int) bool {
		if *pending[i].stopDue == *pending[j].stopDue {
			return guestLess(pending[i].spec, pending[j].spec)
		}
		return *pending[i].stopDue < *pending[j].stopDue
	})

	for _, guest := range pending {
		guest.state = StateStopped
		guest.stopDue = nil
		e.events = append(e.events, Event{
			At:      at,
			Kind:    EventGuestStopped,
			GuestID: guest.spec.ID,
			Guest:   guest.spec.Name,
			Outcome: string(ResultSuccess),
			Detail:  "graceful shutdown completed",
		})
	}
}

func (e *Executor) StartGracefulShutdown(at time.Duration) {
	if e.gracefulStarted {
		return
	}
	e.gracefulStarted = true

	command := fmt.Sprintf(
		"pvenode stopall --force-stop 0 --timeout %d",
		e.config.StopAllTimeoutSeconds,
	)
	e.events = append(e.events, Event{
		At:      at,
		Kind:    EventPVENodeStopAll,
		Command: command,
		Outcome: string(e.config.StopAllResult),
	})
	if e.config.StopAllResult == ResultFailure {
		return
	}

	for _, guest := range e.guests {
		if guest.state == StateStopped {
			continue
		}

		method := shutdownMethod(guest.spec)
		outcome := ResultSuccess
		if guest.spec.GracefulResult == GracefulFailure {
			outcome = ResultFailure
		}
		e.events = append(e.events, Event{
			At:      at,
			Kind:    EventGuestShutdownRequested,
			GuestID: guest.spec.ID,
			Guest:   guest.spec.Name,
			Method:  method,
			Outcome: string(outcome),
		})

		switch guest.spec.GracefulResult {
		case GracefulStops:
			due := at + time.Duration(guest.spec.GracefulDelaySeconds)*time.Second
			guest.state = StateStopping
			guest.stopDue = &due
		case GracefulStuck:
			guest.state = StateStopping
			guest.stopDue = nil
		case GracefulFailure:
			guest.state = StateRunning
		}
	}
	e.Advance(at)
}

func (e *Executor) ForceRemaining(at time.Duration) {
	for _, guest := range e.guests {
		if guest.state == StateStopped {
			continue
		}

		outcome := ResultSuccess
		if guest.spec.EmergencyForceResult == ForceFailure {
			outcome = ResultFailure
		}
		e.events = append(e.events, Event{
			At:      at,
			Kind:    EventGuestEmergencyForce,
			GuestID: guest.spec.ID,
			Guest:   guest.spec.Name,
			Method:  "force_stop",
			Command: forceCommand(guest.spec),
			Outcome: string(outcome),
		})
		if outcome == ResultSuccess {
			guest.state = StateStopped
			guest.stopDue = nil
		}
	}
}

func (e *Executor) RequestHostPoweroff(at time.Duration) CommandResult {
	result := e.config.HostPoweroffResult
	detail := ""
	if !e.AllGuestsStopped() {
		result = ResultBlocked
		detail = "one or more guests are still running"
	}
	e.events = append(e.events, Event{
		At:      at,
		Kind:    EventHostPoweroff,
		Command: "systemctl poweroff",
		Outcome: string(result),
		Detail:  detail,
	})
	return result
}

func (e *Executor) TestAgent(at time.Duration, guestID int) (AgentProbeResult, error) {
	guest := e.findGuest(guestID)
	if guest == nil {
		return "", fmt.Errorf("guest %d not found", guestID)
	}

	result := guest.spec.AgentProbe
	command := ""
	switch {
	case guest.spec.Kind != GuestVM:
		result = AgentNotRelevant
	case !guest.spec.AgentEnabled:
		result = AgentDisabled
	default:
		command = fmt.Sprintf("qm agent %d ping", guest.spec.ID)
	}
	e.events = append(e.events, Event{
		At:      at,
		Kind:    EventAgentTest,
		GuestID: guest.spec.ID,
		Guest:   guest.spec.Name,
		Method:  "qemu_guest_agent",
		Command: command,
		Outcome: string(result),
	})
	guest.lastAgentTest = &AgentTestStatus{At: at, Result: result}
	return result, nil
}

func (e *Executor) AllGuestsStopped() bool {
	for _, guest := range e.guests {
		if guest.state != StateStopped {
			return false
		}
	}
	return true
}

func (e *Executor) Events() []Event {
	return append([]Event(nil), e.events...)
}

func (e *Executor) GuestStatuses() []GuestStatus {
	statuses := make([]GuestStatus, 0, len(e.guests))
	for _, guest := range e.guests {
		var lastAgentTest *AgentTestStatus
		if guest.lastAgentTest != nil {
			copy := *guest.lastAgentTest
			lastAgentTest = &copy
		}
		statuses = append(statuses, GuestStatus{
			ID:            guest.spec.ID,
			Name:          guest.spec.Name,
			Kind:          guest.spec.Kind,
			State:         guest.state,
			LastAgentTest: lastAgentTest,
		})
	}
	return statuses
}

func (e *Executor) findGuest(id int) *guestRuntime {
	for _, guest := range e.guests {
		if guest.spec.ID == id {
			return guest
		}
	}
	return nil
}

func (e *Executor) sortGuests() {
	sort.SliceStable(e.guests, func(i, j int) bool {
		return guestLess(e.guests[i].spec, e.guests[j].spec)
	})
}

func guestLess(left, right Guest) bool {
	if left.ShutdownOrder == right.ShutdownOrder {
		return left.ID < right.ID
	}
	return left.ShutdownOrder < right.ShutdownOrder
}

func shutdownMethod(guest Guest) string {
	if guest.Kind == GuestLXC {
		return "lxc_shutdown"
	}
	if guest.AgentEnabled {
		return "qemu_guest_agent"
	}
	return "acpi"
}

func forceCommand(guest Guest) string {
	if guest.Kind == GuestLXC {
		return fmt.Sprintf("pct stop %d", guest.ID)
	}
	return fmt.Sprintf("qm stop %d", guest.ID)
}

func normalizeConfig(config Config) (Config, error) {
	if config.StopAllResult == "" {
		config.StopAllResult = ResultSuccess
	}
	if config.StopAllTimeoutSeconds == 0 {
		config.StopAllTimeoutSeconds = 180
	}
	if config.HostPoweroffResult == "" {
		config.HostPoweroffResult = ResultSuccess
	}
	if config.StopAllResult != ResultSuccess && config.StopAllResult != ResultFailure {
		return Config{}, fmt.Errorf("unknown stopall result %q", config.StopAllResult)
	}
	if config.HostPoweroffResult != ResultSuccess && config.HostPoweroffResult != ResultFailure {
		return Config{}, fmt.Errorf("unknown host poweroff result %q", config.HostPoweroffResult)
	}
	if config.StopAllTimeoutSeconds <= 0 {
		return Config{}, fmt.Errorf("stopall timeout must be positive")
	}

	seen := make(map[int]struct{}, len(config.Guests))
	for index := range config.Guests {
		guest := &config.Guests[index]
		if guest.ID <= 0 {
			return Config{}, fmt.Errorf("guest ID must be positive")
		}
		if _, exists := seen[guest.ID]; exists {
			return Config{}, fmt.Errorf("duplicate guest ID %d", guest.ID)
		}
		seen[guest.ID] = struct{}{}
		if guest.Name == "" {
			return Config{}, fmt.Errorf("guest %d name is required", guest.ID)
		}
		if guest.Kind != GuestVM && guest.Kind != GuestLXC {
			return Config{}, fmt.Errorf("guest %d has unknown kind %q", guest.ID, guest.Kind)
		}
		if guest.GracefulDelaySeconds < 0 {
			return Config{}, fmt.Errorf("guest %d graceful delay cannot be negative", guest.ID)
		}
		if guest.GracefulResult == "" {
			guest.GracefulResult = GracefulStops
		}
		if guest.GracefulResult != GracefulStops &&
			guest.GracefulResult != GracefulStuck &&
			guest.GracefulResult != GracefulFailure {
			return Config{}, fmt.Errorf("guest %d has unknown graceful result %q", guest.ID, guest.GracefulResult)
		}
		if guest.EmergencyForceResult == "" {
			guest.EmergencyForceResult = ForceStops
		}
		if guest.EmergencyForceResult != ForceStops && guest.EmergencyForceResult != ForceFailure {
			return Config{}, fmt.Errorf("guest %d has unknown force result %q", guest.ID, guest.EmergencyForceResult)
		}
		switch {
		case guest.Kind == GuestLXC:
			guest.AgentProbe = AgentNotRelevant
		case !guest.AgentEnabled:
			guest.AgentProbe = AgentDisabled
		case guest.AgentProbe == "":
			guest.AgentProbe = AgentSuccess
		case guest.AgentProbe != AgentSuccess &&
			guest.AgentProbe != AgentFailure &&
			guest.AgentProbe != AgentTimeout:
			return Config{}, fmt.Errorf("guest %d has unknown agent probe result %q", guest.ID, guest.AgentProbe)
		}
	}
	return config, nil
}
