package managerweb

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

type EventStore struct {
	mu        sync.RWMutex
	filePath  string
	retention time.Duration
	events    []Event
	sequence  uint64
}

func NewEventStore(filePath string, retention time.Duration) (*EventStore, error) {
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	store := &EventStore{
		filePath:  filePath,
		retention: retention,
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *EventStore) Add(event Event) (Event, error) {
	now := time.Now().UTC()
	event.Type = normalizeEventType(event.Type)
	event.Title = strings.TrimSpace(event.Title)
	event.Note = strings.TrimSpace(event.Note)
	event.Source = strings.TrimSpace(event.Source)
	if event.Title == "" {
		return Event{}, fmt.Errorf("event title is required")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = now
	} else {
		event.CreatedAt = event.CreatedAt.UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequence++
	if event.ID == "" {
		event.ID = fmt.Sprintf("%d-%d", event.CreatedAt.UnixNano(), s.sequence)
	}
	s.events = append(s.events, event)
	s.pruneLocked(now.Add(-s.retention))
	if len(s.events) > maxStoredEvents {
		s.events = append([]Event(nil), s.events[len(s.events)-maxStoredEvents:]...)
	}
	if err := s.persistLocked(); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (s *EventStore) List(limit int) []Event {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	before := len(s.events)
	s.pruneLocked(time.Now().UTC().Add(-s.retention))
	if len(s.events) != before {
		_ = s.persistLocked()
	}
	count := len(s.events)
	if count > limit {
		count = limit
	}
	result := make([]Event, 0, count)
	for index := len(s.events) - 1; index >= 0 && len(result) < count; index-- {
		result = append(result, s.events[index])
	}
	return result
}

func (s *EventStore) load() error {
	if s.filePath == "" {
		return nil
	}
	file, err := os.Open(s.filePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open event history: %w", err)
	}
	defer file.Close()

	cutoff := time.Now().UTC().Add(-s.retention)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode event history: %w", err)
		}
		if event.Title != "" && !event.CreatedAt.Before(cutoff) {
			s.events = append(s.events, event)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read event history: %w", err)
	}
	if len(s.events) > maxStoredEvents {
		s.events = append([]Event(nil), s.events[len(s.events)-maxStoredEvents:]...)
	}
	return nil
}

func (s *EventStore) persistLocked() error {
	if s.filePath == "" {
		return nil
	}
	directory := filepath.Dir(s.filePath)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create event history directory: %w", err)
	}
	staged, err := os.CreateTemp(directory, ".events-*.tmp")
	if err != nil {
		return fmt.Errorf("create event history stage: %w", err)
	}
	stagePath := staged.Name()
	defer os.Remove(stagePath)
	if err := staged.Chmod(0o600); err != nil {
		staged.Close()
		return fmt.Errorf("protect event history stage: %w", err)
	}
	writer := bufio.NewWriter(staged)
	for _, event := range s.events {
		encoded, err := json.Marshal(event)
		if err != nil {
			staged.Close()
			return fmt.Errorf("encode event history: %w", err)
		}
		if _, err := writer.Write(append(encoded, '\n')); err != nil {
			staged.Close()
			return fmt.Errorf("write event history: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		staged.Close()
		return fmt.Errorf("flush event history: %w", err)
	}
	if err := staged.Sync(); err != nil {
		staged.Close()
		return fmt.Errorf("sync event history: %w", err)
	}
	if err := staged.Close(); err != nil {
		return fmt.Errorf("close event history: %w", err)
	}
	if err := os.Rename(stagePath, s.filePath); err != nil {
		return fmt.Errorf("replace event history: %w", err)
	}
	return os.Chmod(s.filePath, 0o600)
}

func (s *EventStore) pruneLocked(cutoff time.Time) {
	first := 0
	for first < len(s.events) && s.events[first].CreatedAt.Before(cutoff) {
		first++
	}
	if first > 0 {
		s.events = append([]Event(nil), s.events[first:]...)
	}
}

func normalizeEventType(value string) string {
	switch value {
	case "success", "warning":
		return value
	default:
		return "info"
	}
}
