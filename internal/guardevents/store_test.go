package guardevents

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStorePersistsNewestFirst(t *testing.T) {
	store := &Store{Path: filepath.Join(t.TempDir(), "events.jsonl"), Retention: 24 * time.Hour}
	if err := store.Add(Event{Type: "info", Title: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(Event{Type: "warning", Title: "second"}); err != nil {
		t.Fatal(err)
	}
	events, err := Read(store.Path, 24*time.Hour, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Title != "second" || events[1].Title != "first" {
		t.Fatalf("unexpected events: %#v", events)
	}
	if events[0].Source != "pve-guard" {
		t.Fatalf("unexpected source: %q", events[0].Source)
	}
}
