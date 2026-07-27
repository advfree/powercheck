package pveexec

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"powercheck/internal/readonlyexec"
)

// Runner executes only the small, explicitly validated set of PVE power
// commands used by PowerCheck. It never invokes a shell.
type Runner interface {
	Run(context.Context, string, ...string) (readonlyexec.Output, error)
}

type OSRunner struct{}

func (OSRunner) Run(ctx context.Context, name string, args ...string) (readonlyexec.Output, error) {
	if err := Validate(name, args); err != nil {
		return readonlyexec.Output{}, err
	}

	command := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	output := readonlyexec.Output{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err != nil {
		return output, fmt.Errorf(
			"PVE command %s failed: %w; stderr: %s",
			name,
			err,
			strings.TrimSpace(stderr.String()),
		)
	}
	return output, nil
}

func Validate(name string, args []string) error {
	switch name {
	case "pvenode":
		if len(args) != 5 ||
			args[0] != "stopall" ||
			args[1] != "--force-stop" ||
			args[2] != "0" ||
			args[3] != "--timeout" ||
			!validTimeout(args[4]) {
			return rejected(name, args)
		}
	case "qm", "pct":
		if validGuestShutdown(args) || validGuestStop(args) {
			return nil
		}
		return rejected(name, args)
	case "systemctl":
		if len(args) != 1 || args[0] != "poweroff" {
			return rejected(name, args)
		}
	default:
		return rejected(name, args)
	}
	return nil
}

func validGuestShutdown(args []string) bool {
	return len(args) == 4 &&
		args[0] == "shutdown" &&
		validVMID(args[1]) &&
		args[2] == "--timeout" &&
		validTimeout(args[3])
}

func validGuestStop(args []string) bool {
	return len(args) == 2 && args[0] == "stop" && validVMID(args[1])
}

func validVMID(raw string) bool {
	value, err := strconv.Atoi(raw)
	return err == nil && value >= 100 && value <= 999999999
}

func validTimeout(raw string) bool {
	value, err := strconv.Atoi(raw)
	return err == nil && value >= 1 && value <= 3600
}

func rejected(name string, args []string) error {
	return fmt.Errorf(
		"command rejected by PVE power allowlist: %s %s",
		name,
		strings.Join(args, " "),
	)
}
