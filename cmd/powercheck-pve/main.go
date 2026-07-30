package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"powercheck/internal/buildinfo"
	"powercheck/internal/core"
	"powercheck/internal/dryrun"
	"powercheck/internal/guardevents"
	"powercheck/internal/guardstate"
	"powercheck/internal/nutreader"
	"powercheck/internal/outageconfig"
	"powercheck/internal/pveexec"
	"powercheck/internal/pvereader"
	"powercheck/internal/pveweb"
	"powercheck/internal/reachability"
	"powercheck/internal/readonlyexec"
)

func main() {
	var (
		action          = flag.String("action", "status", "status, agent-test, guest-shutdown, stopall, force-stop, host-poweroff, guard, or web")
		node            = flag.String("node", "", "local PVE node name")
		vmid            = flag.Int("vmid", 0, "target VM or container ID")
		timeoutSeconds  = flag.Int("timeout", 180, "graceful guest shutdown timeout in seconds")
		execute         = flag.Bool("execute", false, "allow the selected write action")
		confirmNode     = flag.String("confirm-node", "", "must exactly match -node for node-wide actions")
		confirmVMID     = flag.Int("confirm-vmid", 0, "must exactly match -vmid for guest actions")
		emergency       = flag.Bool("emergency", false, "required for abrupt guest force-stop")
		confirmPoweroff = flag.Bool("confirm-host-poweroff", false, "required for host poweroff")
		listen          = flag.String("listen", "127.0.0.1:8765", "web console listen address")
		webRoot         = flag.String("web-root", "/usr/local/share/powercheck/web", "directory containing the web console")
		webAccountFile  = flag.String("web-account-file", "/etc/powercheck/web-account.json", "root-readable web account file")
		outageConfig    = flag.String("outage-config", "/etc/powercheck/outage-config.json", "persistent outage timing configuration")
		guardState      = flag.String("guard-state", "/var/lib/powercheck/guard-state.json", "automatic guard state file")
		guardEventFile  = flag.String("guard-event-file", "/var/lib/powercheck/guard-events.jsonl", "automatic guard event history")
		apiOnly         = flag.Bool("api-only", false, "serve the PVE API without web console assets")
		apiAllowSource  = flag.String("api-allow-source", "", "comma-separated source IPs allowed to access API-only mode")
		guardConfirm    = flag.String("confirm-auto-guard", "", `must exactly equal "AUTO SHUTDOWN <node>" for guard mode`)
		guardNUTTarget  = flag.String("guard-nut-target", "ups@192.168.1.200", "NUT target for local automatic guard")
		guardLANTargets = flag.String("guard-lan-targets", "192.168.1.1,192.168.1.200", "comma-separated LAN targets for local automatic guard")
		guardWANTargets = flag.String("guard-wan-targets", "1.1.1.1,223.5.5.5", "comma-separated WAN targets for local automatic guard")
		guardInterval   = flag.Int("guard-interval", 5, "automatic guard sample interval in seconds")
		guardConfirmSec = flag.Int("guard-confirm", 30, "continuous outage confirmation time in seconds")
		guardBudget     = flag.Int("guard-budget", 120, "automatic guard total shutdown budget in seconds")
		guardReserve    = flag.Int("guard-emergency-reserve", 45, "automatic guard emergency reserve in seconds")
		versionOnly     = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *versionOnly {
		fmt.Println(buildinfo.String("powercheck-pve"))
		return
	}
	if *action == "hash-web-password" {
		password, err := readPasswordFromStdin()
		if err != nil {
			exitError(err)
		}
		hash, err := pveweb.HashPassword(password)
		if err != nil {
			exitError(err)
		}
		fmt.Println(hash)
		return
	}
	if *node == "" {
		exitError(fmt.Errorf("-node is required"))
	}
	if *timeoutSeconds < 1 || *timeoutSeconds > 3600 {
		exitError(fmt.Errorf("-timeout must be between 1 and 3600 seconds"))
	}
	localNode, err := os.Hostname()
	if err != nil {
		exitError(fmt.Errorf("read local hostname: %w", err))
	}

	readRunner := readonlyexec.OSRunner{GOOS: runtime.GOOS}
	reader := pvereader.Client{Runner: readRunner, Node: *node}
	executor := &pveexec.Executor{
		Runner:          pveexec.OSRunner{},
		Guests:          reader,
		Node:            *node,
		LocalNode:       localNode,
		ShutdownTimeout: time.Duration(*timeoutSeconds) * time.Second,
	}
	if *action == "web" {
		requireLinuxExecution(*execute)
		requireNodeConfirmation(*node, *confirmNode)
		account, err := readWebAccount(*webAccountFile)
		if err != nil {
			exitError(err)
		}
		server := pveweb.Server{
			Node:           *node,
			Executor:       executor,
			Agents:         reader,
			OutageConfig:   outageconfig.Store{Path: *outageConfig},
			Username:       account.Username,
			PasswordHash:   account.PasswordHash,
			WebRoot:        *webRoot,
			APIOnly:        *apiOnly,
			AllowedSources: splitTargets(*apiAllowSource),
			GuardEventFile: *guardEventFile,
			Logger:         log.New(os.Stdout, "powercheck-web ", log.LstdFlags|log.LUTC),
			ActionLimit:    time.Duration(*timeoutSeconds+30) * time.Second,
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		mode := "web console"
		if *apiOnly {
			mode = "API"
		}
		fmt.Printf("PowerCheck PVE %s listening on http://%s for node %s\n", mode, *listen, *node)
		if err := server.ListenAndServe(ctx, *listen); err != nil {
			exitError(err)
		}
		return
	}
	if *action == "guard" {
		requireLinuxExecution(*execute)
		requireNodeConfirmation(*node, *confirmNode)
		if !*emergency {
			exitError(fmt.Errorf("-emergency is required for automatic guard force-stop fallback"))
		}
		if *guardConfirm != "AUTO SHUTDOWN "+*node {
			exitError(fmt.Errorf("-confirm-auto-guard must exactly equal %q", "AUTO SHUTDOWN "+*node))
		}
		configStore := outageconfig.Store{Path: *outageConfig}
		storedConfig, err := configStore.Load()
		if err != nil {
			exitError(fmt.Errorf("load automatic guard configuration: %w", err))
		}
		if storedConfig.Mode != outageconfig.ModeProduction {
			exitError(fmt.Errorf("automatic guard requires outage configuration mode %q", outageconfig.ModeProduction))
		}
		config := dryrun.Config{
			Detection: core.Config{
				Interval:             time.Duration(*guardInterval) * time.Second,
				NUTConfirm:           time.Duration(*guardConfirmSec) * time.Second,
				NetworkConfirm:       time.Duration(*guardConfirmSec) * time.Second,
				TotalBudget:          time.Duration(*guardBudget) * time.Second,
				EmergencyReserve:     time.Duration(*guardReserve) * time.Second,
				RecoverySuccessCount: 3,
			},
			RoundTimeout:                4 * time.Second,
			PingTimeout:                 time.Second,
			PVECommandTimeout:           3 * time.Second,
			GuestShutdownTimeoutSeconds: int64(*timeoutSeconds),
			PVENode:                     *node,
			NUTTarget:                   *guardNUTTarget,
			LANTargets:                  splitTargets(*guardLANTargets),
			WANTargets:                  splitTargets(*guardWANTargets),
		}
		config.Detection = storedConfig.Detection()
		config.GuestShutdownTimeoutSeconds = storedConfig.GuestShutdownTimeoutSeconds
		if err := config.Validate(); err != nil {
			exitError(fmt.Errorf("validate automatic guard configuration: %w", err))
		}
		nut := nutreader.Client{Runner: readRunner, Target: config.NUTTarget}
		ping := reachability.Prober{
			Runner:  readRunner,
			Timeout: config.PingTimeout,
			GOOS:    runtime.GOOS,
		}
		collector, err := dryrun.NewCollector(config, reader, nut, ping)
		if err != nil {
			exitError(err)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		logger := log.New(os.Stdout, "powercheck-guard ", log.LstdFlags|log.LUTC)
		if err := runAutoGuard(
			ctx,
			config,
			storedConfig.Revision,
			configStore,
			guardstate.Store{Path: *guardState},
			&guardevents.Store{Path: *guardEventFile, Retention: 24 * time.Hour},
			collector,
			executor,
			logger,
		); err != nil {
			exitError(err)
		}
		return
	}

	timeout := 10 * time.Second
	if *action == "guest-shutdown" || *action == "stopall" {
		timeout = time.Duration(*timeoutSeconds+30) * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var (
		result pveexec.Result
		runErr error
	)
	switch *action {
	case "status":
		result, runErr = executor.Status(ctx)
	case "agent-test":
		requireVMID(*vmid)
		agentResult, agentErr := reader.TestAgent(ctx, *vmid)
		writeJSON(struct {
			Action string                    `json:"action"`
			VMID   int                       `json:"vmid"`
			Result pvereader.AgentTestResult `json:"result"`
			Error  string                    `json:"error,omitempty"`
		}{
			Action: "agent-test",
			VMID:   *vmid,
			Result: agentResult,
			Error:  errorText(agentErr),
		})
		if agentErr != nil {
			os.Exit(1)
		}
		return
	case "guest-shutdown":
		requireLinuxExecution(*execute)
		requireVMIDConfirmation(*vmid, *confirmVMID)
		result, runErr = executor.ShutdownGuest(ctx, *vmid)
	case "stopall":
		requireLinuxExecution(*execute)
		requireNodeConfirmation(*node, *confirmNode)
		result, runErr = executor.StopAll(ctx)
	case "force-stop":
		requireLinuxExecution(*execute)
		requireVMIDConfirmation(*vmid, *confirmVMID)
		if !*emergency {
			exitError(fmt.Errorf("-emergency is required for force-stop"))
		}
		result, runErr = executor.ForceStopGuest(ctx, *vmid)
	case "host-poweroff":
		requireLinuxExecution(*execute)
		requireNodeConfirmation(*node, *confirmNode)
		if !*confirmPoweroff {
			exitError(fmt.Errorf("-confirm-host-poweroff is required"))
		}
		result, runErr = executor.PoweroffHost(ctx)
	default:
		exitError(fmt.Errorf("unknown -action %q", *action))
	}
	if runErr != nil {
		writeJSON(struct {
			Result pveexec.Result `json:"result"`
			Error  string         `json:"error"`
		}{Result: result, Error: runErr.Error()})
		os.Exit(1)
	}
	writeJSON(result)
}

func requireLinuxExecution(execute bool) {
	if !execute {
		exitError(fmt.Errorf("write action blocked: add -execute after reviewing the command"))
	}
	if runtime.GOOS != "linux" {
		exitError(fmt.Errorf("write actions are supported only on Linux/PVE"))
	}
}

func requireVMID(vmid int) {
	if vmid < 100 || vmid > 999999999 {
		exitError(fmt.Errorf("-vmid must be between 100 and 999999999"))
	}
}

func requireVMIDConfirmation(vmid, confirmation int) {
	requireVMID(vmid)
	if confirmation != vmid {
		exitError(fmt.Errorf("-confirm-vmid must exactly match -vmid"))
	}
}

func requireNodeConfirmation(node, confirmation string) {
	if confirmation != node {
		exitError(fmt.Errorf("-confirm-node must exactly match -node"))
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func splitTargets(raw string) []string {
	var targets []string
	for _, item := range strings.Split(raw, ",") {
		if value := strings.TrimSpace(item); value != "" {
			targets = append(targets, value)
		}
	}
	return targets
}

type webAccount struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
}

func readWebAccount(path string) (webAccount, error) {
	info, err := os.Stat(path)
	if err != nil {
		return webAccount{}, fmt.Errorf("read web account metadata: %w", err)
	}
	if runtime.GOOS == "linux" && info.Mode().Perm()&0o077 != 0 {
		return webAccount{}, fmt.Errorf("web account file %q must not be readable by group or others", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return webAccount{}, fmt.Errorf("read web account: %w", err)
	}
	var account webAccount
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&account); err != nil {
		return webAccount{}, fmt.Errorf("decode web account: %w", err)
	}
	if account.Username == "" || len(account.Username) > 64 {
		return webAccount{}, fmt.Errorf("web account username is invalid")
	}
	if account.PasswordHash == "" {
		return webAccount{}, fmt.Errorf("web account password_hash is required")
	}
	return account, nil
}

func readPasswordFromStdin() (string, error) {
	content, err := io.ReadAll(io.LimitReader(os.Stdin, 1025))
	if err != nil {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	if len(content) > 1024 {
		return "", fmt.Errorf("web password must not exceed 1024 characters")
	}
	password := strings.TrimRight(string(content), "\r\n")
	if password == "" {
		return "", fmt.Errorf("web password is required on stdin")
	}
	return password, nil
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
