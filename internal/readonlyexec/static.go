package readonlyexec

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
)

// StaticRunner exercises the real parsers without starting operating-system
// processes. It is used by the local dry-run demo.
type StaticRunner struct {
	GOOS           string
	PVEResources   []byte
	NUTVariables   []byte
	Reachable      map[string]bool
	AgentReachable map[int]bool
}

func (r StaticRunner) Run(_ context.Context, name string, args ...string) (Output, error) {
	goos := r.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	if err := Validate(name, args, goos); err != nil {
		return Output{}, err
	}

	switch name {
	case "pvesh":
		return Output{Stdout: append([]byte(nil), r.PVEResources...)}, nil
	case "upsc":
		return Output{Stdout: append([]byte(nil), r.NUTVariables...)}, nil
	case "ping":
		target := args[len(args)-1]
		if r.Reachable[target] {
			return Output{}, nil
		}
		return Output{}, fmt.Errorf("static ping target %s is unreachable", target)
	case "qm":
		vmid, _ := strconv.Atoi(args[1])
		if r.AgentReachable[vmid] {
			return Output{}, nil
		}
		return Output{Stderr: []byte("guest agent is not running")}, fmt.Errorf("static QEMU Guest Agent failure")
	default:
		return Output{}, fmt.Errorf("static runner does not implement %s", name)
	}
}
