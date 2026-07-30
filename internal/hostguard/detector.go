package hostguard

import (
	"fmt"
	"strings"
	"time"

	"powercheck/internal/core"
)

type Sample struct {
	UPSStatus    string
	NUTReachable bool
	LANReachable bool
	WANReachable bool
}

type Detector struct {
	engine *core.Engine
}

func NewDetector(config core.Config) (*Detector, error) {
	engine, err := core.NewEngine(config)
	if err != nil {
		return nil, err
	}
	return &Detector{engine: engine}, nil
}

func NewDetectorFromEngine(engine *core.Engine) (*Detector, error) {
	if engine == nil {
		return nil, fmt.Errorf("host guard engine is required")
	}
	return &Detector{engine: engine}, nil
}

func (d *Detector) Engine() *core.Engine {
	if d == nil {
		return nil
	}
	return d.engine
}

func (d *Detector) Step(at time.Duration, sample Sample) ([]core.Action, error) {
	if d == nil || d.engine == nil {
		return nil, fmt.Errorf("host guard detector is required")
	}
	nut := core.NUTUnreachable
	if sample.NUTReachable {
		nut = parseUPSStatus(sample.UPSStatus)
	}
	return d.engine.Step(at, core.Snapshot{
		NUT:              nut,
		LANReachable:     sample.LANReachable,
		WANReachable:     sample.WANReachable,
		AllGuestsStopped: false,
	})
}

func parseUPSStatus(status string) core.NUTStatus {
	for _, field := range strings.Fields(strings.ToUpper(status)) {
		switch field {
		case string(core.NUTLowBattery):
			return core.NUTLowBattery
		case string(core.NUTOnBattery):
			return core.NUTOnBattery
		}
	}
	for _, field := range strings.Fields(strings.ToUpper(status)) {
		if field == string(core.NUTOnline) {
			return core.NUTOnline
		}
	}
	return core.NUTUnreachable
}

func RequestsPoweroff(actions []core.Action) bool {
	for _, action := range actions {
		if action.Kind == core.ActionGracefulShutdown || action.Kind == core.ActionHostPoweroffRequested {
			return true
		}
	}
	return false
}
