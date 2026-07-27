package reachability

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"powercheck/internal/readonlyexec"
)

type recordingRunner struct {
	name string
	args []string
	err  error
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) (readonlyexec.Output, error) {
	r.name = name
	r.args = append([]string(nil), args...)
	return readonlyexec.Output{}, r.err
}

func TestLinuxPingArguments(t *testing.T) {
	runner := &recordingRunner{}
	prober := Prober{Runner: runner, Timeout: 1500 * time.Millisecond, GOOS: "linux"}
	result := prober.Probe(context.Background(), "192.168.1.1")
	if !result.Reachable {
		t.Fatalf("probe failed: %#v", result)
	}
	if runner.name != "ping" ||
		!reflect.DeepEqual(runner.args, []string{"-c", "1", "-W", "2", "192.168.1.1"}) {
		t.Fatalf("unexpected command: %s %v", runner.name, runner.args)
	}
}

func TestFailedPingIsUnreachable(t *testing.T) {
	runner := &recordingRunner{err: errors.New("timeout")}
	prober := Prober{Runner: runner, Timeout: 2 * time.Second, GOOS: "windows"}
	result := prober.Probe(context.Background(), "1.1.1.1")
	if result.Reachable || result.Error == "" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
