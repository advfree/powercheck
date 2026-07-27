package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"powercheck/internal/buildinfo"
	"powercheck/internal/dryrun"
	"powercheck/internal/nutreader"
	"powercheck/internal/pvereader"
	"powercheck/internal/reachability"
	"powercheck/internal/readonlyexec"
)

func main() {
	var (
		configPath  = flag.String("config", "powercheck-dryrun.example.json", "dry-run JSON configuration")
		watch       = flag.Bool("watch", false, "keep sampling until interrupted")
		agentVMID   = flag.Int("agent-test", 0, "run one read-only QEMU Guest Agent ping")
		demo        = flag.Bool("demo", false, "use built-in command output instead of real programs")
		versionOnly = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *versionOnly {
		fmt.Println(buildinfo.String("powercheck-dryrun"))
		return
	}

	config, err := dryrun.LoadConfig(*configPath)
	if err != nil {
		exitError(err)
	}

	var runner readonlyexec.Runner = readonlyexec.OSRunner{GOOS: runtime.GOOS}
	if *demo {
		runner = demoRunner()
	}
	pve := pvereader.Client{Runner: runner, Node: config.PVENode}
	if *agentVMID != 0 {
		runAgentTest(config, pve, *agentVMID)
		return
	}

	nut := nutreader.Client{Runner: runner, Target: config.NUTTarget}
	ping := reachability.Prober{
		Runner:  runner,
		Timeout: config.PingTimeout,
		GOOS:    runtime.GOOS,
	}
	collector, err := dryrun.NewCollector(config, pve, nut, ping)
	if err != nil {
		exitError(err)
	}
	session, err := dryrun.NewSession(config, collector)
	if err != nil {
		exitError(err)
	}

	if !*watch {
		report, err := session.Sample(context.Background(), 0)
		if err != nil {
			exitError(err)
		}
		report.SingleSampleNote = "one sample cannot confirm an outage; use -watch to exercise confirmation timers"
		writeJSON(report)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runWatch(ctx, session, config.Detection.Interval)
}

func demoRunner() readonlyexec.StaticRunner {
	return readonlyexec.StaticRunner{
		GOOS: runtime.GOOS,
		PVEResources: []byte(`[
			{"vmid":100,"name":"windows","type":"qemu","status":"running","node":"pve"},
			{"vmid":200,"name":"dns","type":"lxc","status":"running","node":"pve"},
			{"vmid":300,"name":"truenas","type":"qemu","status":"running","node":"pve"}
		]`),
		NUTVariables: []byte(
			"battery.charge: 96\n" +
				"battery.runtime: 1800\n" +
				"input.voltage: 229.0\n" +
				"ups.load: 18\n" +
				"ups.status: OL\n",
		),
		Reachable: map[string]bool{
			"192.168.1.1":  true,
			"192.168.1.10": true,
			"223.5.5.5":    true,
			"1.1.1.1":      true,
		},
		AgentReachable: map[int]bool{100: true, 300: true},
	}
}

func runWatch(ctx context.Context, session *dryrun.Session, interval time.Duration) {
	started := time.Now()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		report, err := session.Sample(ctx, time.Since(started))
		if err != nil {
			exitError(err)
		}
		writeJSON(report)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runAgentTest(config dryrun.Config, client pvereader.Client, vmid int) {
	ctx, cancel := context.WithTimeout(context.Background(), config.PVECommandTimeout)
	defer cancel()

	result, err := client.TestAgent(ctx, vmid)
	response := struct {
		Mode   string                    `json:"mode"`
		VMID   int                       `json:"vmid"`
		Result pvereader.AgentTestResult `json:"result"`
		Error  string                    `json:"error,omitempty"`
	}{
		Mode:   "dry-run-agent-test",
		VMID:   vmid,
		Result: result,
	}
	if err != nil {
		response.Error = err.Error()
	}
	writeJSON(response)
	if err != nil {
		os.Exit(1)
	}
}

func writeJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		exitError(err)
	}
}

func exitError(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
