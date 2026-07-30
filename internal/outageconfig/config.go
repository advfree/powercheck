package outageconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"powercheck/internal/core"
)

const (
	ModeDryRun     = "dry-run"
	ModeProduction = "production"
)

const (
	defaultIntervalSeconds             = 5
	defaultNUTConfirmSeconds           = 30
	defaultNetworkConfirmSeconds       = 30
	defaultTotalBudgetSeconds          = 120
	defaultEmergencyReserveSeconds     = 45
	defaultRecoverySuccessCount        = 3
	defaultGuestShutdownTimeoutSeconds = 45
)

type Config struct {
	Mode                        string    `json:"mode"`
	Revision                    int       `json:"revision"`
	UpdatedAt                   time.Time `json:"updated_at,omitempty"`
	IntervalSeconds             int64     `json:"interval_seconds"`
	NUTConfirmSeconds           int64     `json:"nut_confirm_seconds"`
	NetworkConfirmSeconds       int64     `json:"network_confirm_seconds"`
	TotalBudgetSeconds          int64     `json:"total_budget_seconds"`
	EmergencyReserveSeconds     int64     `json:"emergency_reserve_seconds"`
	RecoverySuccessCount        int       `json:"recovery_success_count"`
	GuestShutdownTimeoutSeconds int64     `json:"guest_shutdown_timeout_seconds"`
}

func Default() Config {
	return Config{
		Mode:                        ModeProduction,
		IntervalSeconds:             defaultIntervalSeconds,
		NUTConfirmSeconds:           defaultNUTConfirmSeconds,
		NetworkConfirmSeconds:       defaultNetworkConfirmSeconds,
		TotalBudgetSeconds:          defaultTotalBudgetSeconds,
		EmergencyReserveSeconds:     defaultEmergencyReserveSeconds,
		RecoverySuccessCount:        defaultRecoverySuccessCount,
		GuestShutdownTimeoutSeconds: defaultGuestShutdownTimeoutSeconds,
	}
}

func (c Config) Detection() core.Config {
	return core.Config{
		Interval:             time.Duration(c.IntervalSeconds) * time.Second,
		NUTConfirm:           time.Duration(c.NUTConfirmSeconds) * time.Second,
		NetworkConfirm:       time.Duration(c.NetworkConfirmSeconds) * time.Second,
		TotalBudget:          time.Duration(c.TotalBudgetSeconds) * time.Second,
		EmergencyReserve:     time.Duration(c.EmergencyReserveSeconds) * time.Second,
		RecoverySuccessCount: c.RecoverySuccessCount,
	}
}

func (c Config) Validate() error {
	switch {
	case c.Mode != ModeDryRun && c.Mode != ModeProduction:
		return fmt.Errorf("mode must be %q or %q", ModeDryRun, ModeProduction)
	case c.Revision < 0:
		return fmt.Errorf("revision cannot be negative")
	case c.IntervalSeconds < 5 || c.IntervalSeconds > 60:
		return fmt.Errorf("interval_seconds must be between 5 and 60")
	case c.NUTConfirmSeconds < 5 || c.NUTConfirmSeconds > 600:
		return fmt.Errorf("nut_confirm_seconds must be between 5 and 600")
	case c.NetworkConfirmSeconds < 5 || c.NetworkConfirmSeconds > 600:
		return fmt.Errorf("network_confirm_seconds must be between 5 and 600")
	case c.TotalBudgetSeconds < 60 || c.TotalBudgetSeconds > 3600:
		return fmt.Errorf("total_budget_seconds must be between 60 and 3600")
	case c.EmergencyReserveSeconds < 10 || c.EmergencyReserveSeconds > 900:
		return fmt.Errorf("emergency_reserve_seconds must be between 10 and 900")
	case c.RecoverySuccessCount < 1 || c.RecoverySuccessCount > 20:
		return fmt.Errorf("recovery_success_count must be between 1 and 20")
	case c.GuestShutdownTimeoutSeconds < 10 || c.GuestShutdownTimeoutSeconds > 3600:
		return fmt.Errorf("guest_shutdown_timeout_seconds must be between 10 and 3600")
	}
	if err := c.Detection().Validate(); err != nil {
		return fmt.Errorf("invalid detection timing: %w", err)
	}
	return nil
}

type Store struct {
	Path string
}

func (s Store) Load() (Config, error) {
	if s.Path == "" {
		return Config{}, fmt.Errorf("outage config path is required")
	}
	content, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read outage config: %w", err)
	}
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode outage config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values are not allowed")
		}
		return Config{}, fmt.Errorf("decode outage config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate outage config: %w", err)
	}
	return config, nil
}

func (s Store) Save(config Config) error {
	if s.Path == "" {
		return fmt.Errorf("outage config path is required")
	}
	if err := config.Validate(); err != nil {
		return err
	}
	directory := filepath.Dir(s.Path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create outage config directory: %w", err)
	}
	stage, err := os.CreateTemp(directory, ".outage-config-*.tmp")
	if err != nil {
		return fmt.Errorf("create outage config stage: %w", err)
	}
	stagePath := stage.Name()
	cleanup := func() {
		_ = stage.Close()
		_ = os.Remove(stagePath)
	}
	defer cleanup()
	if err := stage.Chmod(0o600); err != nil {
		return fmt.Errorf("protect outage config stage: %w", err)
	}
	encoder := json.NewEncoder(stage)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		return fmt.Errorf("encode outage config: %w", err)
	}
	if err := stage.Sync(); err != nil {
		return fmt.Errorf("sync outage config: %w", err)
	}
	if err := stage.Close(); err != nil {
		return fmt.Errorf("close outage config: %w", err)
	}
	if err := os.Rename(stagePath, s.Path); err != nil {
		return fmt.Errorf("replace outage config: %w", err)
	}
	if err := os.Chmod(s.Path, 0o600); err != nil {
		return fmt.Errorf("protect outage config: %w", err)
	}
	return nil
}
