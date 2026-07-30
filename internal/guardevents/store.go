package guardevents

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxStoredEvents = 1000

type Event struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Note      string    `json:"note"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

type Store struct {
	Path      string
	Retention time.Duration
	mu        sync.Mutex
	sequence  uint64
}

func (s *Store) Add(event Event) error {
	if s == nil || s.Path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	event.CreatedAt = now
	event.Title = strings.TrimSpace(event.Title)
	event.Note = strings.TrimSpace(event.Note)
	if event.Title == "" {
		return fmt.Errorf("guard event title is required")
	}
	switch event.Type {
	case "success", "warning":
	default:
		event.Type = "info"
	}
	event.Source = "pve-guard"
	s.sequence++
	event.ID = fmt.Sprintf("guard-%d-%d", now.UnixNano(), s.sequence)
	events, err := Read(s.Path, s.retention(), maxStoredEvents)
	if err != nil {
		return err
	}
	events = append(events, event)
	sort.Slice(events, func(i, j int) bool {
		return events[i].CreatedAt.Before(events[j].CreatedAt)
	})
	if len(events) > maxStoredEvents {
		events = events[len(events)-maxStoredEvents:]
	}
	return write(s.Path, events)
}

func Read(path string, retention time.Duration, limit int) ([]Event, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open guard events: %w", err)
	}
	defer file.Close()
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	cutoff := time.Now().UTC().Add(-retention)
	var events []Event
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("decode guard events: %w", err)
		}
		if event.Title != "" && !event.CreatedAt.Before(cutoff) {
			events = append(events, event)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read guard events: %w", err)
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].CreatedAt.After(events[j].CreatedAt)
	})
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func (s *Store) retention() time.Duration {
	if s.Retention <= 0 {
		return 24 * time.Hour
	}
	return s.Retention
}

func write(path string, events []Event) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create guard event directory: %w", err)
	}
	stage, err := os.CreateTemp(directory, ".guard-events-*.tmp")
	if err != nil {
		return fmt.Errorf("create guard event stage: %w", err)
	}
	stagePath := stage.Name()
	defer func() {
		_ = stage.Close()
		_ = os.Remove(stagePath)
	}()
	if err := stage.Chmod(0o600); err != nil {
		return fmt.Errorf("protect guard event stage: %w", err)
	}
	writer := bufio.NewWriter(stage)
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("encode guard event: %w", err)
		}
		if _, err := writer.Write(append(encoded, '\n')); err != nil {
			return fmt.Errorf("write guard event: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush guard events: %w", err)
	}
	if err := stage.Sync(); err != nil {
		return fmt.Errorf("sync guard events: %w", err)
	}
	if err := stage.Close(); err != nil {
		return fmt.Errorf("close guard events: %w", err)
	}
	if err := os.Rename(stagePath, path); err != nil {
		return fmt.Errorf("replace guard events: %w", err)
	}
	return os.Chmod(path, 0o600)
}
