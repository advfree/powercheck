package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"powercheck/internal/buildinfo"
	"powercheck/internal/pveexec"
	"powercheck/internal/pvereader"
	"powercheck/internal/pveweb"
	"powercheck/internal/readonlyexec"
)

func main() {
	var (
		action          = flag.String("action", "status", "status, agent-test, guest-shutdown, stopall, force-stop, host-poweroff, or web")
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
		webUser         = flag.String("web-user", "admin", "web console Basic Auth username")
		webPasswordFile = flag.String("web-password-file", "/etc/powercheck/web-password", "root-readable web password file")
		versionOnly     = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *versionOnly {
		fmt.Println(buildinfo.String("powercheck-pve"))
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
	executor := pveexec.Executor{
		Runner:          pveexec.OSRunner{},
		Guests:          reader,
		Node:            *node,
		LocalNode:       localNode,
		ShutdownTimeout: time.Duration(*timeoutSeconds) * time.Second,
	}
	if *action == "web" {
		requireLinuxExecution(*execute)
		requireNodeConfirmation(*node, *confirmNode)
		password, err := readWebPassword(*webPasswordFile)
		if err != nil {
			exitError(err)
		}
		server := pveweb.Server{
			Node:        *node,
			Executor:    executor,
			Agents:      reader,
			Username:    *webUser,
			Password:    password,
			WebRoot:     *webRoot,
			Logger:      log.New(os.Stdout, "powercheck-web ", log.LstdFlags|log.LUTC),
			ActionLimit: time.Duration(*timeoutSeconds+30) * time.Second,
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		fmt.Printf("PowerCheck PVE web console listening on http://%s for node %s\n", *listen, *node)
		if err := server.ListenAndServe(ctx, *listen); err != nil {
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

func readWebPassword(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read web password metadata: %w", err)
	}
	if runtime.GOOS == "linux" && info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("web password file %q must not be readable by group or others", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read web password: %w", err)
	}
	password := strings.TrimSpace(string(content))
	if len(password) < 12 {
		return "", fmt.Errorf("web password must contain at least 12 characters")
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
