package guardstate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"powercheck/internal/core"
)

const defaultBootIDPath = "/proc/sys/kernel/random/boot_id"

type Snapshot struct {
	BootID         string      `json:"boot_id"`
	Origin         time.Time   `json:"origin"`
	ConfigRevision int         `json:"config_revision"`
	Detection      core.Config `json:"detection"`
	Status         core.Status `json:"status"`
	LastAt         *int64      `json:"last_at_nanoseconds,omitempty"`
}

type Store struct {
	Path       string
	BootIDPath string
}

func (s Store) Restore() (*core.Engine, core.Config, time.Time, int, bool, error) {
	snapshot, found, err := s.load()
	if err != nil || !found {
		return nil, core.Config{}, time.Time{}, 0, false, err
	}
	bootID, err := s.currentBootID()
	if err != nil {
		return nil, core.Config{}, time.Time{}, 0, false, err
	}
	if snapshot.BootID != bootID ||
		snapshot.Origin.IsZero() ||
		(snapshot.Status.State != core.StateConfirming && snapshot.Status.State != core.StateShuttingDown) {
		if err := s.Clear(); err != nil {
			return nil, core.Config{}, time.Time{}, 0, false, err
		}
		return nil, core.Config{}, time.Time{}, 0, false, nil
	}
	var lastAt *time.Duration
	if snapshot.LastAt != nil {
		value := time.Duration(*snapshot.LastAt)
		lastAt = &value
	}
	engine, err := core.RestoreEngine(snapshot.Detection, snapshot.Status, lastAt)
	if err != nil {
		return nil, core.Config{}, time.Time{}, 0, false, fmt.Errorf("restore guard engine: %w", err)
	}
	return engine, snapshot.Detection, snapshot.Origin, snapshot.ConfigRevision, true, nil
}

func (s Store) Save(origin time.Time, configRevision int, detection core.Config, engine *core.Engine) error {
	if s.Path == "" {
		return fmt.Errorf("guard state path is required")
	}
	if engine == nil {
		return fmt.Errorf("guard engine is required")
	}
	bootID, err := s.currentBootID()
	if err != nil {
		return err
	}
	status := engine.Status()
	if status.State == core.StateNormal || status.State == core.StatePoweredOff {
		return s.Clear()
	}
	var lastAt *int64
	if value := engine.LastAt(); value != nil {
		nanoseconds := int64(*value)
		lastAt = &nanoseconds
	}
	return s.write(Snapshot{
		BootID:         bootID,
		Origin:         origin,
		ConfigRevision: configRevision,
		Detection:      detection,
		Status:         status,
		LastAt:         lastAt,
	})
}

func (s Store) Clear() error {
	if s.Path == "" {
		return fmt.Errorf("guard state path is required")
	}
	if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove guard state: %w", err)
	}
	return nil
}

func (s Store) currentBootID() (string, error) {
	path := s.BootIDPath
	if path == "" {
		path = defaultBootIDPath
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read boot ID: %w", err)
	}
	value := strings.TrimSpace(string(content))
	if value == "" {
		return "", fmt.Errorf("boot ID is empty")
	}
	return value, nil
}

func (s Store) load() (Snapshot, bool, error) {
	if s.Path == "" {
		return Snapshot{}, false, fmt.Errorf("guard state path is required")
	}
	content, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("read guard state: %w", err)
	}
	var snapshot Snapshot
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, false, fmt.Errorf("decode guard state: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values are not allowed")
		}
		return Snapshot{}, false, fmt.Errorf("decode guard state: %w", err)
	}
	return snapshot, true, nil
}

func (s Store) write(snapshot Snapshot) error {
	directory := filepath.Dir(s.Path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create guard state directory: %w", err)
	}
	stage, err := os.CreateTemp(directory, ".guard-state-*.tmp")
	if err != nil {
		return fmt.Errorf("create guard state stage: %w", err)
	}
	stagePath := stage.Name()
	defer func() {
		_ = stage.Close()
		_ = os.Remove(stagePath)
	}()
	if err := stage.Chmod(0o600); err != nil {
		return fmt.Errorf("protect guard state stage: %w", err)
	}
	encoder := json.NewEncoder(stage)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(snapshot); err != nil {
		return fmt.Errorf("encode guard state: %w", err)
	}
	if err := stage.Sync(); err != nil {
		return fmt.Errorf("sync guard state: %w", err)
	}
	if err := stage.Close(); err != nil {
		return fmt.Errorf("close guard state: %w", err)
	}
	if err := os.Rename(stagePath, s.Path); err != nil {
		return fmt.Errorf("replace guard state: %w", err)
	}
	if err := os.Chmod(s.Path, 0o600); err != nil {
		return fmt.Errorf("protect guard state: %w", err)
	}
	return nil
}
