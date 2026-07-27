package pveweb

import (
	"context"
	"encoding/base64"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"powercheck/internal/pveexec"
	"powercheck/internal/pvereader"
)

type fakeBackend struct {
	mu       sync.Mutex
	guests   []pvereader.Guest
	actions  []string
	block    chan struct{}
	poweroff bool
}

func (b *fakeBackend) Status(context.Context) (pveexec.Result, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	guests := append([]pvereader.Guest(nil), b.guests...)
	return pveexec.Result{
		Action:           "status",
		Guests:           guests,
		AllGuestsStopped: pvereader.AllGuestsStopped(guests),
	}, nil
}

func (b *fakeBackend) TestAgent(context.Context, int) (pvereader.AgentTestResult, error) {
	return pvereader.AgentSuccess, nil
}

func (b *fakeBackend) ShutdownGuest(_ context.Context, vmid int) (pveexec.Result, error) {
	b.mu.Lock()
	b.actions = append(b.actions, "guest-shutdown")
	for index := range b.guests {
		if b.guests[index].VMID == vmid {
			b.guests[index].Status = "stopped"
		}
	}
	b.mu.Unlock()
	if b.block != nil {
		<-b.block
	}
	return pveexec.Result{Action: "guest-shutdown", Executed: true}, nil
}

func (b *fakeBackend) StopAll(context.Context) (pveexec.Result, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.actions = append(b.actions, "stopall")
	for index := range b.guests {
		b.guests[index].Status = "stopped"
	}
	return pveexec.Result{Action: "stopall", Executed: true, AllGuestsStopped: true}, nil
}

func (b *fakeBackend) PoweroffHost(context.Context) (pveexec.Result, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.actions = append(b.actions, "host-poweroff")
	if !pvereader.AllGuestsStopped(b.guests) {
		return pveexec.Result{}, errors.New("guests still running")
	}
	b.poweroff = true
	return pveexec.Result{Action: "host-poweroff", Executed: true}, nil
}

func TestServerRequiresAuthentication(t *testing.T) {
	server, _ := testServer(t, &fakeBackend{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestStatusAndAgentTestAreAvailableThroughAuthenticatedAPI(t *testing.T) {
	backend := &fakeBackend{guests: []pvereader.Guest{
		{VMID: 100, Name: "windows", Type: pvereader.GuestQEMU, Status: "running"},
	}}
	server, _ := testServer(t, backend)

	response := doRequest(t, server, http.MethodGet, "/api/v1/status", "", true, false)
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"vmid":100`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	response = doRequest(t, server, http.MethodPost, "/api/v1/agent-test", `{"vmid":100}`, true, true)
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"agent_result":"success"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestWriteActionRequiresHeaderAndMatchingConfirmation(t *testing.T) {
	backend := &fakeBackend{guests: []pvereader.Guest{
		{VMID: 100, Type: pvereader.GuestQEMU, Status: "running"},
	}}
	server, _ := testServer(t, backend)

	response := doRequest(t, server, http.MethodPost, "/api/v1/guest-shutdown", `{"vmid":100,"confirm_vmid":100}`, true, false)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing header status=%d", response.Code)
	}
	response = doRequest(t, server, http.MethodPost, "/api/v1/guest-shutdown", `{"vmid":100,"confirm_vmid":101}`, true, true)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("wrong confirmation status=%d", response.Code)
	}
	if len(backend.actions) != 0 {
		t.Fatalf("action ran without valid confirmation: %#v", backend.actions)
	}

	response = doRequest(t, server, http.MethodPost, "/api/v1/guest-shutdown", `{"vmid":100,"confirm_vmid":100}`, true, true)
	if response.Code != http.StatusOK || len(backend.actions) != 1 {
		t.Fatalf("status=%d actions=%#v body=%s", response.Code, backend.actions, response.Body.String())
	}
}

func TestHostPoweroffRequiresTypedPhrase(t *testing.T) {
	backend := &fakeBackend{guests: []pvereader.Guest{
		{VMID: 100, Type: pvereader.GuestQEMU, Status: "stopped"},
	}}
	server, _ := testServer(t, backend)
	wrong := `{"confirm_node":"pve","confirm_poweroff":"POWER OFF"}`
	response := doRequest(t, server, http.MethodPost, "/api/v1/host-poweroff", wrong, true, true)
	if response.Code != http.StatusBadRequest || backend.poweroff {
		t.Fatalf("status=%d poweroff=%v", response.Code, backend.poweroff)
	}
	right := `{"confirm_node":"pve","confirm_poweroff":"POWER OFF pve"}`
	response = doRequest(t, server, http.MethodPost, "/api/v1/host-poweroff", right, true, true)
	if response.Code != http.StatusOK || !backend.poweroff {
		t.Fatalf("status=%d poweroff=%v body=%s", response.Code, backend.poweroff, response.Body.String())
	}
}

func TestOnlyOneWriteActionCanRunAtATime(t *testing.T) {
	block := make(chan struct{})
	backend := &fakeBackend{
		guests: []pvereader.Guest{{VMID: 100, Type: pvereader.GuestQEMU, Status: "running"}},
		block:  block,
	}
	server, _ := testServer(t, backend)
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		firstDone <- doRequest(t, server, http.MethodPost, "/api/v1/guest-shutdown", `{"vmid":100,"confirm_vmid":100}`, true, true)
	}()
	deadline := time.Now().Add(time.Second)
	for {
		backend.mu.Lock()
		started := len(backend.actions) == 1
		backend.mu.Unlock()
		if started {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first action did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	second := doRequest(t, server, http.MethodPost, "/api/v1/stopall", `{"confirm_node":"pve"}`, true, true)
	if second.Code != http.StatusConflict {
		t.Fatalf("second action status=%d body=%s", second.Code, second.Body.String())
	}
	close(block)
	if first := <-firstDone; first.Code != http.StatusOK {
		t.Fatalf("first action status=%d", first.Code)
	}
}

func testServer(t *testing.T, backend *fakeBackend) (http.Handler, *strings.Builder) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<h1>PowerCheck</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs strings.Builder
	server := &Server{
		Node:        "pve",
		Executor:    backend,
		Agents:      backend,
		Username:    "admin",
		Password:    "a-secure-test-password",
		WebRoot:     root,
		Logger:      log.New(&logs, "", 0),
		ActionLimit: time.Second,
	}
	handler, err := server.Handler()
	if err != nil {
		t.Fatal(err)
	}
	return handler, &logs
}

func doRequest(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	body string,
	auth bool,
	confirmed bool,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if auth {
		request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:a-secure-test-password")))
	}
	if confirmed {
		request.Header.Set(confirmationHeader, "confirmed")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
