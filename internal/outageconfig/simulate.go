package outageconfig

import (
	"fmt"
	"strconv"
	"time"

	"powercheck/internal/core"
	"powercheck/internal/pvereader"
)

const (
	ScenarioNUT     = "nut-ob"
	ScenarioNetwork = "network-loss"
)

type SimulationStep struct {
	AtSeconds int64           `json:"at_seconds"`
	Kind      core.ActionKind `json:"kind"`
	Reason    string          `json:"reason"`
	WouldRun  []string        `json:"would_run,omitempty"`
}

type Simulation struct {
	Mode       string           `json:"mode"`
	Scenario   string           `json:"scenario"`
	Config     Config           `json:"config"`
	Steps      []SimulationStep `json:"steps"`
	FinishedAt int64            `json:"finished_at_seconds"`
}

func Simulate(config Config, scenario string, guests []pvereader.Guest) (Simulation, error) {
	if err := config.Validate(); err != nil {
		return Simulation{}, err
	}
	if scenario != ScenarioNUT && scenario != ScenarioNetwork {
		return Simulation{}, fmt.Errorf("scenario must be %q or %q", ScenarioNUT, ScenarioNetwork)
	}
	engine, err := core.NewEngine(config.Detection())
	if err != nil {
		return Simulation{}, err
	}
	snapshot := core.Snapshot{
		NUT:              core.NUTOnBattery,
		LANReachable:     true,
		WANReachable:     true,
		AllGuestsStopped: pvereader.AllGuestsStopped(guests),
	}
	if scenario == ScenarioNetwork {
		snapshot.NUT = core.NUTUnreachable
		snapshot.LANReachable = false
		snapshot.WANReachable = false
	}

	result := Simulation{Mode: ModeDryRun, Scenario: scenario, Config: config}
	limit := time.Duration(config.TotalBudgetSeconds) * time.Second
	interval := time.Duration(config.IntervalSeconds) * time.Second
	for at := time.Duration(0); at <= limit; at += interval {
		actions, stepErr := engine.Step(at, snapshot)
		if stepErr != nil {
			return Simulation{}, stepErr
		}
		for _, action := range actions {
			step := SimulationStep{
				AtSeconds: int64(action.At / time.Second),
				Kind:      action.Kind,
				Reason:    action.Reason,
			}
			switch action.Kind {
			case core.ActionGracefulShutdown:
				step.WouldRun = []string{fmt.Sprintf(
					"pvenode stopall --force-stop 0 --timeout %d",
					config.GuestShutdownTimeoutSeconds,
				)}
			case core.ActionEmergencyStopRemaining:
				step.WouldRun = emergencyCommands(guests)
			case core.ActionHostPoweroffRequested:
				step.WouldRun = []string{"systemctl poweroff"}
			}
			result.Steps = append(result.Steps, step)
			result.FinishedAt = step.AtSeconds
		}
		if engine.Status().State == core.StatePoweredOff {
			break
		}
	}
	return result, nil
}

func emergencyCommands(guests []pvereader.Guest) []string {
	var commands []string
	for _, guest := range guests {
		if guest.Template || guest.Status == "stopped" {
			continue
		}
		command := "qm stop "
		if guest.Type == pvereader.GuestLXC {
			command = "pct stop "
		}
		commands = append(commands, command+strconv.Itoa(guest.VMID))
	}
	return commands
}
