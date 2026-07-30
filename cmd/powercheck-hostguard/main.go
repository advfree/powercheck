package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"powercheck/internal/buildinfo"
	"powercheck/internal/core"
	"powercheck/internal/guardstate"
	"powercheck/internal/hostguard"
	"powercheck/internal/nutnetwork"
	"powercheck/internal/pveexec"
	"powercheck/internal/reachability"
	"powercheck/internal/readonlyexec"
)

type liveSampler struct {
	nut        nutnetwork.Client
	ping       reachability.Prober
	lanTargets []string
	wanTargets []string
}

func main() {
	var (
		host          = flag.String("host", "", "exact local hostname")
		confirmHost   = flag.String("confirm-host", "", "must exactly match -host")
		confirmGuard  = flag.String("confirm-auto-guard", "", `must exactly equal "AUTO SHUTDOWN <host>"`)
		execute       = flag.Bool("execute", false, "allow local host poweroff")
		nutAddress    = flag.String("nut-address", "192.168.1.200:3493", "NUT server address")
		nutName       = flag.String("nut-name", "", "NUT UPS name, empty to discover")
		lanTargetsRaw = flag.String("lan-targets", "192.168.1.1,192.168.1.200,192.168.1.66", "comma-separated LAN targets")
		wanTargetsRaw = flag.String("wan-targets", "223.5.5.5,119.29.29.29", "comma-separated WAN targets")
		intervalSec   = flag.Int("interval", 5, "sample interval in seconds")
		confirmSec    = flag.Int("confirm", 30, "continuous outage confirmation in seconds")
		statePath     = flag.String("state", "/var/lib/powercheck/host-guard-state.json", "host guard state file")
		versionOnly   = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()
	if *versionOnly {
		fmt.Println(buildinfo.String("powercheck-hostguard"))
		return
	}
	if runtime.GOOS != "linux" {
		exitError(fmt.Errorf("host guard is supported only on Linux"))
	}
	localHost, err := os.Hostname()
	if err != nil {
		exitError(fmt.Errorf("read local hostname: %w", err))
	}
	if *host == "" || *host != localHost || *confirmHost != *host {
		exitError(fmt.Errorf("-host and -confirm-host must exactly match local hostname %q", localHost))
	}
	if !*execute || *confirmGuard != "AUTO SHUTDOWN "+*host {
		exitError(fmt.Errorf("real host guard requires -execute and confirmation %q", "AUTO SHUTDOWN "+*host))
	}
	if *intervalSec < 1 || *intervalSec > 60 || *confirmSec < 5 || *confirmSec > 600 {
		exitError(fmt.Errorf("invalid host guard interval or confirmation time"))
	}

	config := core.Config{
		Interval:             time.Duration(*intervalSec) * time.Second,
		NUTConfirm:           time.Duration(*confirmSec) * time.Second,
		NetworkConfirm:       time.Duration(*confirmSec) * time.Second,
		TotalBudget:          120 * time.Second,
		EmergencyReserve:     45 * time.Second,
		RecoverySuccessCount: 3,
	}
	stateStore := guardstate.Store{Path: *statePath}
	engine, restoredConfig, started, _, restored, err := stateStore.Restore()
	if err != nil {
		exitError(err)
	}
	var detector *hostguard.Detector
	if restored {
		config = restoredConfig
		detector, err = hostguard.NewDetectorFromEngine(engine)
	} else {
		detector, err = hostguard.NewDetector(config)
		started = time.Now()
	}
	if err != nil {
		exitError(err)
	}
	readRunner := readonlyexec.OSRunner{GOOS: runtime.GOOS}
	sampler := liveSampler{
		nut: nutnetwork.Client{
			Address: *nutAddress,
			UPSName: *nutName,
			Timeout: 3 * time.Second,
		},
		ping: reachability.Prober{
			Runner:  readRunner,
			Timeout: time.Second,
			GOOS:    runtime.GOOS,
		},
		lanTargets: splitTargets(*lanTargetsRaw),
		wanTargets: splitTargets(*wanTargetsRaw),
	}
	if len(sampler.lanTargets) == 0 || len(sampler.wanTargets) == 0 {
		exitError(fmt.Errorf("at least one LAN and WAN target is required"))
	}
	logger := log.New(os.Stdout, "powercheck-hostguard ", log.LstdFlags|log.LUTC)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, detector, sampler, pveexec.OSRunner{}, config, started, stateStore, logger); err != nil {
		exitError(err)
	}
}

func run(
	ctx context.Context,
	detector *hostguard.Detector,
	sampler liveSampler,
	powerRunner pveexec.Runner,
	config core.Config,
	started time.Time,
	stateStore guardstate.Store,
	logger *log.Logger,
) error {
	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()
	logger.Printf("armed host-only interval=%s confirm=%s restored=%t", config.Interval, config.NUTConfirm, detector.Engine().LastAt() != nil)
	for {
		sample, issues := sampler.sample(ctx)
		for _, issue := range issues {
			logger.Printf("sample issue=%q", issue)
		}
		actions, err := detector.Step(time.Since(started), sample)
		if err != nil {
			return err
		}
		for _, action := range actions {
			logger.Printf(
				"action kind=%s at=%s reason=%q nut_reachable=%t status=%q lan=%t wan=%t",
				action.Kind,
				action.At.Round(time.Second),
				action.Reason,
				sample.NUTReachable,
				sample.UPSStatus,
				sample.LANReachable,
				sample.WANReachable,
			)
		}
		if err := stateStore.Save(started, 0, config, detector.Engine()); err != nil {
			return fmt.Errorf("persist host guard state: %w", err)
		}
		if hostguard.RequestsPoweroff(actions) {
			powerContext, cancel := context.WithTimeout(ctx, 20*time.Second)
			_, powerErr := powerRunner.Run(powerContext, "systemctl", "poweroff")
			cancel()
			if powerErr != nil {
				return fmt.Errorf("power off local host: %w", powerErr)
			}
			logger.Printf("local host poweroff accepted")
			return nil
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s liveSampler) sample(parent context.Context) (hostguard.Sample, []string) {
	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()
	var sample hostguard.Sample
	status, err := s.nut.Read(ctx)
	if err == nil {
		sample.NUTReachable = true
		sample.UPSStatus = status.UPSStatus
		sample.LANReachable = true
		sample.WANReachable = true
		return sample, nil
	}
	issues := []string{"NUT: " + err.Error()}
	var mutex sync.Mutex
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		reachable, targetIssues := s.anyReachable(ctx, s.lanTargets)
		mutex.Lock()
		sample.LANReachable = reachable
		issues = append(issues, targetIssues...)
		mutex.Unlock()
	}()
	go func() {
		defer group.Done()
		reachable, targetIssues := s.anyReachable(ctx, s.wanTargets)
		mutex.Lock()
		sample.WANReachable = reachable
		issues = append(issues, targetIssues...)
		mutex.Unlock()
	}()
	group.Wait()
	return sample, issues
}

func (s liveSampler) anyReachable(ctx context.Context, targets []string) (bool, []string) {
	reachable := false
	var issues []string
	for _, target := range targets {
		result := s.ping.Probe(ctx, target)
		if result.Reachable {
			reachable = true
		}
		if result.Error != "" {
			issues = append(issues, target+": "+result.Error)
		}
	}
	return reachable, issues
}

func splitTargets(raw string) []string {
	var targets []string
	for _, target := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(target); trimmed != "" {
			targets = append(targets, trimmed)
		}
	}
	return targets
}

func exitError(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
