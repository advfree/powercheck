package managerweb

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEventStorePersistsNewestFirstAndPrunesOldRecords(t *testing.T) {
	t.Parallel()
	filePath := filepath.Join(t.TempDir(), "events", "events.jsonl")
	store, err := NewEventStore(filePath, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-25 * time.Hour)
	if _, err := store.Add(Event{Type: "warning", Title: "old", CreatedAt: old}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add(Event{Type: "invalid", Title: "first", Source: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add(Event{Type: "success", Title: "second", Source: "test"}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewEventStore(filePath, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	events := reloaded.List(50)
	if len(events) != 2 {
		t.Fatalf("events=%#v", events)
	}
	if events[0].Title != "second" || events[1].Title != "first" {
		t.Fatalf("unexpected order: %#v", events)
	}
	if events[1].Type != "info" {
		t.Fatalf("event type=%q", events[1].Type)
	}
}

func TestEventStorePrunesOnReadWithoutANewerWrite(t *testing.T) {
	t.Parallel()
	store, err := NewEventStore("", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add(Event{Title: "expires"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if events := store.List(50); len(events) != 0 {
		t.Fatalf("events=%#v", events)
	}
}

func TestEventAPIUsesRealNUTPVEAndScanActivity(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/v1/session" {
			if cookie, err := request.Cookie("powercheck_session"); err != nil || cookie.Value != "valid" {
				writer.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(writer, `{"authenticated":false}`)
				return
			}
			_, _ = io.WriteString(writer, `{"authenticated":true}`)
			return
		}
		if request.URL.Path == "/api/v1/status" {
			_, _ = io.WriteString(writer, `{"node":"pve","result":{"guests":[]}}`)
			return
		}
		http.NotFound(writer, request)
	}))
	defer upstream.Close()

	webRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("manager"), 0o644); err != nil {
		t.Fatal(err)
	}
	upstreamURL, err := ParseUpstream(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	events, err := NewEventStore(filepath.Join(t.TempDir(), "events.jsonl"), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := (&Server{
		Upstream: upstreamURL,
		WebRoot:  webRoot,
		NUT:      nutStub{},
		Events:   events,
	}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/manager/v1/nut", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("NUT status=%d body=%s", response.Code, response.Body.String())
		}
	}
	pveResponse := httptest.NewRecorder()
	handler.ServeHTTP(pveResponse, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	if pveResponse.Code != http.StatusOK {
		t.Fatalf("PVE status=%d body=%s", pveResponse.Code, pveResponse.Body.String())
	}

	scanRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/manager/v1/events/scan",
		strings.NewReader(`{"success":true}`),
	)
	scanRequest.AddCookie(&http.Cookie{Name: "powercheck_session", Value: "valid"})
	scanRequest.Header.Set("Content-Type", "application/json")
	scanRequest.Header.Set("X-PowerCheck-Action", "record-scan")
	scanResponse := httptest.NewRecorder()
	handler.ServeHTTP(scanResponse, scanRequest)
	if scanResponse.Code != http.StatusCreated {
		t.Fatalf("scan status=%d body=%s", scanResponse.Code, scanResponse.Body.String())
	}

	eventRequest := httptest.NewRequest(http.MethodGet, "/api/manager/v1/events?limit=100", nil)
	eventRequest.AddCookie(&http.Cookie{Name: "powercheck_session", Value: "valid"})
	eventResponse := httptest.NewRecorder()
	handler.ServeHTTP(eventResponse, eventRequest)
	if eventResponse.Code != http.StatusOK {
		t.Fatalf("events status=%d body=%s", eventResponse.Code, eventResponse.Body.String())
	}
	var payload struct {
		RetentionHours int     `json:"retention_hours"`
		Events         []Event `json:"events"`
	}
	if err := json.Unmarshal(eventResponse.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RetentionHours != 24 || len(payload.Events) != 4 {
		t.Fatalf("payload=%#v", payload)
	}
	titles := make([]string, 0, len(payload.Events))
	for _, event := range payload.Events {
		titles = append(titles, event.Title)
	}
	joined := strings.Join(titles, "\n")
	for _, expected := range []string{
		"PowerCheck Manager 已启动",
		"UPS 市电状态正常：OL",
		"PVE 状态读取正常",
		"实时状态检查完成",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in %#v", expected, titles)
		}
	}
}
