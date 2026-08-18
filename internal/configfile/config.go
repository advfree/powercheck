package configfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"powercheck/internal/core"
)

// File uses seconds instead of time.Duration nanoseconds so the JSON remains
// safe and understandable when edited by hand.
type File struct {
	IntervalSeconds         *int64 `json:"interval_seconds,omitempty"`
	NUTConfirmSeconds       *int64 `json:"nut_confirm_seconds,omitempty"`
	NetworkConfirmSeconds   *int64 `json:"network_confirm_seconds,omitempty"`
	TotalBudgetSeconds      *int64 `json:"total_budget_seconds,omitempty"`
	EmergencyReserveSeconds *int64 `json:"emergency_reserve_seconds,omitempty"`
	RecoverySuccessCount    *int   `json:"recovery_success_count,omitempty"`
}

func Load(path string, defaults core.Config) (core.Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return core.Config{}, err
	}

	var file File
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return core.Config{}, fmt.Errorf("decode %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values are not allowed")
		}
		return core.Config{}, fmt.Errorf("decode %s: %w", path, err)
	}

	config := defaults
	setDuration(&config.Interval, file.IntervalSeconds)
	setDuration(&config.NUTConfirm, file.NUTConfirmSeconds)
	setDuration(&config.NetworkConfirm, file.NetworkConfirmSeconds)
	setDuration(&config.TotalBudget, file.TotalBudgetSeconds)
	setDuration(&config.EmergencyReserve, file.EmergencyReserveSeconds)
	if file.RecoverySuccessCount != nil {
		config.RecoverySuccessCount = *file.RecoverySuccessCount
	}
	if err := config.Validate(); err != nil {
		return core.Config{}, fmt.Errorf("validate %s: %w", path, err)
	}
	return config, nil
}

func setDuration(target *time.Duration, seconds *int64) {
	if seconds != nil {
		*target = time.Duration(*seconds) * time.Second
	}
}
