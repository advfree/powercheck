package probe

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type probeFunc struct {
	name string
	run  func(context.Context) error
}

func (p probeFunc) Name() string {
	return p.name
}

func (p probeFunc) Check(ctx context.Context) error {
	return p.run(ctx)
}

func TestRunnerExecutesProbesConcurrently(t *testing.T) {
	runner := &Runner{
		Timeout:     time.Second,
		MaxParallel: 4,
	}

	probes := make([]Probe, 4)
	for i := range probes {
		probes[i] = probeFunc{
			name: string(rune('a' + i)),
			run: func(ctx context.Context) error {
				select {
				case <-time.After(80 * time.Millisecond):
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		}
	}

	start := time.Now()
	results, err := runner.RunRound(context.Background(), probes)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed >= 250*time.Millisecond {
		t.Fatalf("probes appear sequential; elapsed %s", elapsed)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}
}

func TestRunnerRejectsOverlappingRounds(t *testing.T) {
	runner := &Runner{
		Timeout:     300 * time.Millisecond,
		MaxParallel: 1,
	}

	started := make(chan struct{})
	var once atomic.Bool
	blocking := probeFunc{
		name: "blocking",
		run: func(ctx context.Context) error {
			if once.CompareAndSwap(false, true) {
				close(started)
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}

	done := make(chan error, 1)
	go func() {
		_, err := runner.RunRound(context.Background(), []Probe{blocking})
		done <- err
	}()
	<-started

	if _, err := runner.RunRound(context.Background(), []Probe{blocking}); !errors.Is(err, ErrRoundInProgress) {
		t.Fatalf("expected ErrRoundInProgress, got %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("first round returned unexpected runner error: %v", err)
	}
}
