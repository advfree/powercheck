package probe

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var ErrRoundInProgress = errors.New("probe round already in progress")

type Probe interface {
	Name() string
	Check(context.Context) error
}

type Result struct {
	Name    string
	Success bool
	Latency time.Duration
	Err     error
}

type Runner struct {
	Timeout     time.Duration
	MaxParallel int
	running     atomic.Bool
}

func (r *Runner) RunRound(parent context.Context, probes []Probe) ([]Result, error) {
	if !r.running.CompareAndSwap(false, true) {
		return nil, ErrRoundInProgress
	}
	defer r.running.Store(false)

	if r.Timeout <= 0 {
		return nil, fmt.Errorf("timeout must be positive")
	}
	if r.MaxParallel <= 0 {
		return nil, fmt.Errorf("max parallel probes must be positive")
	}

	ctx, cancel := context.WithTimeout(parent, r.Timeout)
	defer cancel()

	results := make([]Result, len(probes))
	sem := make(chan struct{}, r.MaxParallel)
	var wg sync.WaitGroup

	for index, item := range probes {
		wg.Add(1)
		go func(i int, p Probe) {
			defer wg.Done()
			started := time.Now()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[i] = Result{Name: p.Name(), Err: ctx.Err(), Latency: time.Since(started)}
				return
			}

			err := p.Check(ctx)
			results[i] = Result{
				Name:    p.Name(),
				Success: err == nil,
				Latency: time.Since(started),
				Err:     err,
			}
		}(index, item)
	}

	wg.Wait()
	return results, nil
}
