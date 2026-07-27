package readonlyexec

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

type Output struct {
	Stdout []byte
	Stderr []byte
}

type Runner interface {
	Run(context.Context, string, ...string) (Output, error)
}

type OSRunner struct {
	GOOS string
}

func (r OSRunner) Run(ctx context.Context, name string, args ...string) (Output, error) {
	goos := r.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	if err := Validate(name, args, goos); err != nil {
		return Output{}, err
	}

	command := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	stdout, err := command.Output()
	output := Output{Stdout: stdout, Stderr: stderr.Bytes()}
	if err != nil {
		return output, fmt.Errorf("read-only command %s failed: %w", name, err)
	}
	return output, nil
}

func Validate(name string, args []string, goos string) error {
	switch name {
	case "pvesh":
		expected := []string{"get", "/cluster/resources", "--type", "vm", "--output-format", "json"}
		if !equalArgs(args, expected) {
			return rejected(name, args)
		}
	case "qm":
		if len(args) != 3 || args[0] != "agent" || args[2] != "ping" || !validVMID(args[1]) {
			return rejected(name, args)
		}
	case "upsc":
		if len(args) != 1 || !validUPSTarget(args[0]) {
			return rejected(name, args)
		}
	case "ping":
		if !validPingArgs(args, goos) {
			return rejected(name, args)
		}
	default:
		return rejected(name, args)
	}
	return nil
}

func rejected(name string, args []string) error {
	return fmt.Errorf("command rejected by read-only allowlist: %s %s", name, strings.Join(args, " "))
}

func equalArgs(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range expected {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}

func validVMID(raw string) bool {
	id, err := strconv.Atoi(raw)
	return err == nil && id >= 100 && id <= 999999999
}

var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func validUPSTarget(target string) bool {
	parts := strings.Split(target, "@")
	if len(parts) > 2 || !safeName.MatchString(parts[0]) {
		return false
	}
	if len(parts) == 1 {
		return true
	}
	host, port, hasPort := strings.Cut(parts[1], ":")
	if !validHost(host) {
		return false
	}
	if !hasPort {
		return true
	}
	number, err := strconv.Atoi(port)
	return err == nil && number >= 1 && number <= 65535
}

func validPingArgs(args []string, goos string) bool {
	switch goos {
	case "linux":
		if len(args) != 5 || args[0] != "-c" || args[1] != "1" || args[2] != "-W" {
			return false
		}
		if seconds, err := strconv.Atoi(args[3]); err != nil || seconds < 1 || seconds > 60 {
			return false
		}
		return validHost(args[4])
	case "windows":
		if len(args) != 5 || args[0] != "-n" || args[1] != "1" || args[2] != "-w" {
			return false
		}
		if milliseconds, err := strconv.Atoi(args[3]); err != nil || milliseconds < 1 || milliseconds > 60000 {
			return false
		}
		return validHost(args[4])
	default:
		return false
	}
}

func validHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	return len(host) <= 253 && safeName.MatchString(host)
}
