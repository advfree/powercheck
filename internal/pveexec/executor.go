package pveexec

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"powercheck/internal/pvereader"
)

type GuestReader interface {
	ListGuests(context.Context) ([]pvereader.Guest, error)
}

type Executor struct {
	Runner          Runner
	Guests          GuestReader
	Node            string
	LocalNode       string
	ShutdownTimeout time.Duration
}

type Result struct {
	Action           string            `json:"action"`
	Executed         bool              `json:"executed"`
	Command          []string          `json:"command,omitempty"`
	Guest            *pvereader.Guest  `json:"guest,omitempty"`
	Guests           []pvereader.Guest `json:"guests,omitempty"`
	AllGuestsStopped bool              `json:"all_guests_stopped"`
	Message          string            `json:"message"`
}

func (e *Executor) SetShutdownTimeout(timeout time.Duration) {
	e.ShutdownTimeout = timeout
}

func (e Executor) Status(ctx context.Context) (Result, error) {
	if err := e.validate(); err != nil {
		return Result{}, err
	}
	guests, err := e.Guests.ListGuests(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list PVE guests: %w", err)
	}
	return Result{
		Action:           "status",
		Guests:           guests,
		AllGuestsStopped: pvereader.AllGuestsStopped(guests),
		Message:          "read-only PVE status",
	}, nil
}

func (e Executor) ShutdownGuest(ctx context.Context, vmid int) (Result, error) {
	if err := e.validate(); err != nil {
		return Result{}, err
	}
	if err := e.validateWriteTarget(); err != nil {
		return Result{}, err
	}
	guest, err := e.findGuest(ctx, vmid)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Action:  "guest-shutdown",
		Guest:   &guest,
		Message: "guest was already stopped",
	}
	if guest.Status == "stopped" {
		result.AllGuestsStopped, _ = e.allGuestsStopped(ctx)
		return result, nil
	}

	program := "qm"
	if guest.Type == pvereader.GuestLXC {
		program = "pct"
	}
	args := []string{
		"shutdown",
		strconv.Itoa(vmid),
		"--timeout",
		strconv.Itoa(timeoutSeconds(e.ShutdownTimeout)),
	}
	result.Command = append([]string{program}, args...)
	if _, err := e.Runner.Run(ctx, program, args...); err != nil {
		return result, err
	}
	result.Executed = true

	after, err := e.findGuest(ctx, vmid)
	if err != nil {
		return result, fmt.Errorf("verify guest %d after shutdown: %w", vmid, err)
	}
	result.Guest = &after
	if after.Status != "stopped" {
		return result, fmt.Errorf(
			"guest %d still reports status %q after graceful shutdown",
			vmid,
			after.Status,
		)
	}
	result.AllGuestsStopped, err = e.allGuestsStopped(ctx)
	if err != nil {
		return result, err
	}
	result.Message = "guest stopped gracefully and status was verified"
	return result, nil
}

func (e Executor) StopAll(ctx context.Context) (Result, error) {
	if err := e.validate(); err != nil {
		return Result{}, err
	}
	if err := e.validateWriteTarget(); err != nil {
		return Result{}, err
	}
	args := []string{
		"stopall",
		"--force-stop",
		"0",
		"--timeout",
		strconv.Itoa(timeoutSeconds(e.ShutdownTimeout)),
	}
	result := Result{
		Action:  "stopall",
		Command: append([]string{"pvenode"}, args...),
	}
	if _, err := e.Runner.Run(ctx, "pvenode", args...); err != nil {
		return result, err
	}
	result.Executed = true

	guests, err := e.Guests.ListGuests(ctx)
	if err != nil {
		return result, fmt.Errorf("verify PVE guests after stopall: %w", err)
	}
	result.Guests = guests
	result.AllGuestsStopped = pvereader.AllGuestsStopped(guests)
	if !result.AllGuestsStopped {
		result.Message = "stopall completed but one or more guests are still running"
		return result, errors.New(result.Message)
	}
	result.Message = "all guests stopped gracefully and status was verified"
	return result, nil
}

func (e Executor) ForceStopGuest(ctx context.Context, vmid int) (Result, error) {
	if err := e.validate(); err != nil {
		return Result{}, err
	}
	if err := e.validateWriteTarget(); err != nil {
		return Result{}, err
	}
	guest, err := e.findGuest(ctx, vmid)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Action:  "force-stop",
		Guest:   &guest,
		Message: "guest was already stopped",
	}
	if guest.Status == "stopped" {
		result.AllGuestsStopped, _ = e.allGuestsStopped(ctx)
		return result, nil
	}

	program := "qm"
	if guest.Type == pvereader.GuestLXC {
		program = "pct"
	}
	args := []string{"stop", strconv.Itoa(vmid)}
	result.Command = append([]string{program}, args...)
	if _, err := e.Runner.Run(ctx, program, args...); err != nil {
		return result, err
	}
	result.Executed = true

	after, err := e.findGuest(ctx, vmid)
	if err != nil {
		return result, fmt.Errorf("verify guest %d after force stop: %w", vmid, err)
	}
	result.Guest = &after
	if after.Status != "stopped" {
		return result, fmt.Errorf(
			"guest %d still reports status %q after force stop",
			vmid,
			after.Status,
		)
	}
	result.AllGuestsStopped, err = e.allGuestsStopped(ctx)
	if err != nil {
		return result, err
	}
	result.Message = "guest was force-stopped and status was verified"
	return result, nil
}

func (e Executor) PoweroffHost(ctx context.Context) (Result, error) {
	if err := e.validate(); err != nil {
		return Result{}, err
	}
	if err := e.validateWriteTarget(); err != nil {
		return Result{}, err
	}
	guests, err := e.Guests.ListGuests(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("verify PVE guests before host poweroff: %w", err)
	}
	result := Result{
		Action:           "host-poweroff",
		Command:          []string{"systemctl", "poweroff"},
		Guests:           guests,
		AllGuestsStopped: pvereader.AllGuestsStopped(guests),
	}
	if !result.AllGuestsStopped {
		result.Message = "host poweroff blocked because one or more guests are running"
		return result, errors.New(result.Message)
	}
	if _, err := e.Runner.Run(ctx, "systemctl", "poweroff"); err != nil {
		return result, err
	}
	result.Executed = true
	result.Message = "host poweroff command accepted after all guests were verified stopped"
	return result, nil
}

func (e Executor) validate() error {
	switch {
	case e.Runner == nil:
		return fmt.Errorf("PVE command runner is required")
	case e.Guests == nil:
		return fmt.Errorf("PVE guest reader is required")
	case e.Node == "":
		return fmt.Errorf("PVE node name is required")
	case e.ShutdownTimeout < time.Second:
		return fmt.Errorf("shutdown timeout must be at least one second")
	case e.ShutdownTimeout > time.Hour:
		return fmt.Errorf("shutdown timeout must not exceed one hour")
	}
	return nil
}

func (e Executor) findGuest(ctx context.Context, vmid int) (pvereader.Guest, error) {
	if vmid < 100 || vmid > 999999999 {
		return pvereader.Guest{}, fmt.Errorf("invalid VMID %d", vmid)
	}
	guests, err := e.Guests.ListGuests(ctx)
	if err != nil {
		return pvereader.Guest{}, fmt.Errorf("list PVE guests: %w", err)
	}
	for _, guest := range guests {
		if guest.VMID != vmid {
			continue
		}
		if guest.Template {
			return pvereader.Guest{}, fmt.Errorf("VMID %d is a template", vmid)
		}
		if guest.Node != "" && guest.Node != e.Node {
			return pvereader.Guest{}, fmt.Errorf(
				"VMID %d belongs to node %q, not confirmed node %q",
				vmid,
				guest.Node,
				e.Node,
			)
		}
		return guest, nil
	}
	return pvereader.Guest{}, fmt.Errorf("VMID %d was not found on node %q", vmid, e.Node)
}

func (e Executor) validateWriteTarget() error {
	if canonicalNode(e.LocalNode) == "" {
		return fmt.Errorf("local hostname is unavailable; write action blocked")
	}
	if canonicalNode(e.Node) != canonicalNode(e.LocalNode) {
		return fmt.Errorf(
			"confirmed PVE node %q does not match local hostname %q",
			e.Node,
			e.LocalNode,
		)
	}
	return nil
}

func (e Executor) allGuestsStopped(ctx context.Context) (bool, error) {
	guests, err := e.Guests.ListGuests(ctx)
	if err != nil {
		return false, fmt.Errorf("list PVE guests: %w", err)
	}
	return pvereader.AllGuestsStopped(guests), nil
}

func canonicalNode(value string) string {
	short, _, _ := strings.Cut(strings.TrimSpace(value), ".")
	return strings.ToLower(short)
}

func timeoutSeconds(value time.Duration) int {
	return int((value + time.Second - 1) / time.Second)
}
