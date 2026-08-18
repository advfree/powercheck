package dryrun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"powercheck/internal/core"
	"powercheck/internal/readonlyexec"
)

type Config struct {
	Detection                   core.Config
	RoundTimeout                time.Duration
	PingTimeout                 time.Duration
	PVECommandTimeout           time.Duration
	GuestShutdownTimeoutSeconds int64
	PVENode                     string
	NUTTarget                   string
	LANTargets                  []string
	WANTargets                  []string
}

type configFile struct {
	Mode                        string   `json:"mode"`
	IntervalSeconds             *int64   `json:"interval_seconds,omitempty"`
	NUTConfirmSeconds           *int64   `json:"nut_confirm_seconds,omitempty"`
	NetworkConfirmSeconds       *int64   `json:"network_confirm_seconds,omitempty"`
	TotalBudgetSeconds          *int64   `json:"total_budget_seconds,omitempty"`
	EmergencyReserveSeconds     *int64   `json:"emergency_reserve_seconds,omitempty"`
	RecoverySuccessCount        *int     `json:"recovery_success_count,omitempty"`
	RoundTimeoutSeconds         *int64   `json:"round_timeout_seconds,omitempty"`
	PingTimeoutSeconds          *int64   `json:"ping_timeout_seconds,omitempty"`
	PVECommandTimeoutSeconds    *int64   `json:"pve_command_timeout_seconds,omitempty"`
	GuestShutdownTimeoutSeconds *int64   `json:"guest_shutdown_timeout_seconds,omitempty"`
	PVENode                     string   `json:"pve_node"`
	NUTTarget                   string   `json:"nut_target"`
	LANTargets                  []string `json:"lan_targets"`
	WANTargets                  []string `json:"wan_targets"`
}

func LoadConfig(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var file configFile
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return Config{}, fmt.Errorf("decode %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values are not allowed")
		}
		return Config{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if file.Mode != "dry-run" {
		return Config{}, fmt.Errorf("mode must be exactly %q", "dry-run")
	}

	detection := core.DefaultConfig()
	setDuration(&detection.Interval, file.IntervalSeconds)
	setDuration(&detection.NUTConfirm, file.NUTConfirmSeconds)
	setDuration(&detection.NetworkConfirm, file.NetworkConfirmSeconds)
	setDuration(&detection.TotalBudget, file.TotalBudgetSeconds)
	setDuration(&detection.EmergencyReserve, file.EmergencyReserveSeconds)
	if file.RecoverySuccessCount != nil {
		detection.RecoverySuccessCount = *file.RecoverySuccessCount
	}

	config := Config{
		Detection:                   detection,
		RoundTimeout:                durationOrDefault(file.RoundTimeoutSeconds, 3*time.Second),
		PingTimeout:                 durationOrDefault(file.PingTimeoutSeconds, 2*time.Second),
		PVECommandTimeout:           durationOrDefault(file.PVECommandTimeoutSeconds, 3*time.Second),
		GuestShutdownTimeoutSeconds: integerOrDefault(file.GuestShutdownTimeoutSeconds, 180),
		PVENode:                     file.PVENode,
		NUTTarget:                   file.NUTTarget,
		LANTargets:                  append([]string(nil), file.LANTargets...),
		WANTargets:                  append([]string(nil), file.WANTargets...),
	}
	if err := config.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate %s: %w", path, err)
	}
	return config, nil
}

func (c Config) Validate() error {
	if err := c.Detection.Validate(); err != nil {
		return err
	}
	switch {
	case c.RoundTimeout <= 0:
		return fmt.Errorf("round timeout must be positive")
	case c.PingTimeout <= 0:
		return fmt.Errorf("ping timeout must be positive")
	case c.PVECommandTimeout <= 0:
		return fmt.Errorf("PVE command timeout must be positive")
	case c.Detection.Interval <= c.RoundTimeout:
		return fmt.Errorf("detection interval must exceed round timeout")
	case c.RoundTimeout < c.PingTimeout:
		return fmt.Errorf("round timeout must not be shorter than ping timeout")
	case c.GuestShutdownTimeoutSeconds <= 0:
		return fmt.Errorf("guest shutdown timeout must be positive")
	case c.PVENode == "":
		return fmt.Errorf("PVE node name is required")
	case len(c.LANTargets) == 0:
		return fmt.Errorf("at least one LAN target is required")
	case len(c.WANTargets) < 2:
		return fmt.Errorf("at least two WAN targets are required")
	}
	if err := readonlyexec.Validate("upsc", []string{c.NUTTarget}, "linux"); err != nil {
		return fmt.Errorf("invalid NUT target: %w", err)
	}
	if err := readonlyexec.Validate(
		"ping",
		[]string{"-c", "1", "-W", "2", c.PVENode},
		"linux",
	); err != nil {
		return fmt.Errorf("invalid PVE node name %q", c.PVENode)
	}
	for _, target := range append(append([]string(nil), c.LANTargets...), c.WANTargets...) {
		args := []string{"-c", "1", "-W", "2", target}
		if err := readonlyexec.Validate("ping", args, "linux"); err != nil {
			return fmt.Errorf("invalid ping target %q: %w", target, err)
		}
	}
	return nil
}

func setDuration(target *time.Duration, seconds *int64) {
	if seconds != nil {
		*target = time.Duration(*seconds) * time.Second
	}
}

func durationOrDefault(seconds *int64, fallback time.Duration) time.Duration {
	if seconds == nil {
		return fallback
	}
	return time.Duration(*seconds) * time.Second
}

func integerOrDefault(value *int64, fallback int64) int64 {
	if value == nil {
		return fallback
	}
	return *value
}
