package reachability

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"time"

	"powercheck/internal/readonlyexec"
)

type Result struct {
	Target    string `json:"target"`
	Reachable bool   `json:"reachable"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type Prober struct {
	Runner  readonlyexec.Runner
	Timeout time.Duration
	GOOS    string
}

func (p Prober) Probe(ctx context.Context, target string) Result {
	started := time.Now()
	result := Result{Target: target}
	if p.Runner == nil {
		result.Error = "ping command runner is required"
		return result
	}
	args, err := p.arguments(target)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	_, err = p.Runner.Run(ctx, "ping", args...)
	result.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Reachable = true
	return result
}

func (p Prober) arguments(target string) ([]string, error) {
	goos := p.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	timeout := p.Timeout
	if timeout <= 0 {
		return nil, fmt.Errorf("ping timeout must be positive")
	}
	switch goos {
	case "linux":
		seconds := int((timeout + time.Second - 1) / time.Second)
		return []string{"-c", "1", "-W", strconv.Itoa(seconds), target}, nil
	case "windows":
		return []string{"-n", "1", "-w", strconv.Itoa(int(timeout / time.Millisecond)), target}, nil
	default:
		return nil, fmt.Errorf("unsupported ping platform %q", goos)
	}
}
