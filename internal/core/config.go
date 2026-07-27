package core

import (
	"fmt"
	"time"
)

// Config controls the detector and the absolute shutdown budget.
type Config struct {
	Interval             time.Duration
	NUTConfirm           time.Duration
	NetworkConfirm       time.Duration
	TotalBudget          time.Duration
	EmergencyReserve     time.Duration
	RecoverySuccessCount int
}

func DefaultConfig() Config {
	return Config{
		Interval:             5 * time.Second,
		NUTConfirm:           30 * time.Second,
		NetworkConfirm:       60 * time.Second,
		TotalBudget:          300 * time.Second,
		EmergencyReserve:     60 * time.Second,
		RecoverySuccessCount: 3,
	}
}

func (c Config) Validate() error {
	switch {
	case c.Interval <= 0:
		return fmt.Errorf("interval must be positive")
	case c.NUTConfirm <= 0:
		return fmt.Errorf("NUT confirmation time must be positive")
	case c.NetworkConfirm <= 0:
		return fmt.Errorf("network confirmation time must be positive")
	case c.TotalBudget <= 0:
		return fmt.Errorf("total budget must be positive")
	case c.EmergencyReserve <= 0:
		return fmt.Errorf("emergency reserve must be positive")
	case c.TotalBudget <= c.EmergencyReserve:
		return fmt.Errorf("total budget must exceed emergency reserve")
	case c.TotalBudget-c.EmergencyReserve <= c.NUTConfirm:
		return fmt.Errorf("NUT confirmation leaves no graceful shutdown time")
	case c.TotalBudget-c.EmergencyReserve <= c.NetworkConfirm:
		return fmt.Errorf("network confirmation leaves no graceful shutdown time")
	case c.RecoverySuccessCount <= 0:
		return fmt.Errorf("recovery success count must be positive")
	default:
		return nil
	}
}
