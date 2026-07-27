package pveweb

import (
	"context"
	"errors"
	"fmt"
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

func TestLoginSessionAndLogout(t *testing.T) {
	server, _ := testServer(t, &fakeBackend{})
	wrong := doRequest(t, server, http.MethodPost, "/api/v1/session", `{"username":"admin","password":"wrong-password"}`, nil, false)
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status=%d body=%s", wrong.Code, wrong.Body.String())
	}
	cookie := login(t, server)
	current := doRequest(t, server, http.MethodGet, "/api/v1/session", "", cookie, false)
	if current.Code != http.StatusOK ||
		!strings.Contains(current.Body.String(), `"username":"admin"`) {
		t.Fatalf("session status=%d body=%s", current.Code, current.Body.String())
	}
	logout := doRequest(t, server, http.MethodDelete, "/api/v1/session", "", cookie, false)
	if logout.Code != http.StatusOK {
		t.Fatalf("logout status=%d body=%s", logout.Code, logout.Body.String())
	}
	after := doRequest(t, server, http.MethodGet, "/api/v1/status", "", cookie, false)
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("expired session status=%d body=%s", after.Code, after.Body.String())
	}
}

func TestLoginTemporarilyBlocksRepeatedFailures(t *testing.T) {
	server, _ := testServer(t, &fakeBackend{})
	for attempt := 0; attempt < loginFailureLimit; attempt++ {
		response := doRequest(t, server, http.MethodPost, "/api/v1/session", `{"username":"admin","password":"wrong-password"}`, nil, false)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt=%d status=%d body=%s", attempt+1, response.Code, response.Body.String())
		}
	}
	blocked := doRequest(t, server, http.MethodPost, "/api/v1/session", `{"username":"admin","password":"a-secure-test-password"}`, nil, false)
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked status=%d body=%s", blocked.Code, blocked.Body.String())
	}
}

func TestStatusAndAgentTestAreAvailableThroughSession(t *testing.T) {
	backend := &fakeBackend{guests: []pvereader.Guest{
		{VMID: 100, Name: "windows", Type: pvereader.GuestQEMU, Status: "running", Node: "pve"},
	}}
	server, _ := testServer(t, backend)
	cookie := login(t, server)

	response := doRequest(t, server, http.MethodGet, "/api/v1/status", "", cookie, false)
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"vmid":100`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	response = doRequest(t, server, http.MethodPost, "/api/v1/agent-test", `{"vmid":100}`, cookie, true)
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"agent_result":"success"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAgentTestRejectsLXCAndUnknownTargets(t *testing.T) {
	backend := &fakeBackend{guests: []pvereader.Guest{
		{VMID: 200, Name: "dns", Type: pvereader.GuestLXC, Status: "running", Node: "pve"},
	}}
	server, _ := testServer(t, backend)
	cookie := login(t, server)
	for _, vmid := range []int{200, 999} {
		response := doRequest(t, server, http.MethodPost, "/api/v1/agent-test", fmt.Sprintf(`{"vmid":%d}`, vmid), cookie, true)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("vmid=%d status=%d body=%s", vmid, response.Code, response.Body.String())
		}
	}
}

func TestWriteActionRequiresHeaderAndMatchingConfirmation(t *testing.T) {
	backend := &fakeBackend{guests: []pvereader.Guest{
		{VMID: 100, Type: pvereader.GuestQEMU, Status: "running"},
	}}
	server, _ := testServer(t, backend)
	cookie := login(t, server)

	response := doRequest(t, server, http.MethodPost, "/api/v1/guest-shutdown", `{"vmid":100,"confirm_vmid":100}`, cookie, false)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing header status=%d", response.Code)
	}
	response = doRequest(t, server, http.MethodPost, "/api/v1/guest-shutdown", `{"vmid":100,"confirm_vmid":101}`, cookie, true)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("wrong confirmation status=%d", response.Code)
	}
	if len(backend.actions) != 0 {
		t.Fatalf("action ran without valid confirmation: %#v", backend.actions)
	}

	response = doRequest(t, server, http.MethodPost, "/api/v1/guest-shutdown", `{"vmid":100,"confirm_vmid":100}`, cookie, true)
	if response.Code != http.StatusOK || len(backend.actions) != 1 {
		t.Fatalf("status=%d actions=%#v body=%s", response.Code, backend.actions, response.Body.String())
	}
}

func TestHostPoweroffRequiresTypedPhrase(t *testing.T) {
	backend := &fakeBackend{guests: []pvereader.Guest{
		{VMID: 100, Type: pvereader.GuestQEMU, Status: "stopped"},
	}}
	server, _ := testServer(t, backend)
	cookie := login(t, server)
	wrong := `{"confirm_node":"pve","confirm_poweroff":"POWER OFF"}`
	response := doRequest(t, server, http.MethodPost, "/api/v1/host-poweroff", wrong, cookie, true)
	if response.Code != http.StatusBadRequest || backend.poweroff {
		t.Fatalf("status=%d poweroff=%v", response.Code, backend.poweroff)
	}
	right := `{"confirm_node":"pve","confirm_poweroff":"POWER OFF pve"}`
	response = doRequest(t, server, http.MethodPost, "/api/v1/host-poweroff", right, cookie, true)
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
	cookie := login(t, server)
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		firstDone <- doRequest(t, server, http.MethodPost, "/api/v1/guest-shutdown", `{"vmid":100,"confirm_vmid":100}`, cookie, true)
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
	second := doRequest(t, server, http.MethodPost, "/api/v1/stopall", `{"confirm_node":"pve"}`, cookie, true)
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
	passwordHash, err := HashPassword("a-secure-test-password")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		Node:         "pve",
		Executor:     backend,
		Agents:       backend,
		Username:     "admin",
		PasswordHash: passwordHash,
		WebRoot:      root,
		Logger:       log.New(&logs, "", 0),
		ActionLimit:  time.Second,
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
	cookie *http.Cookie,
	confirmed bool,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if confirmed {
		request.Header.Set(confirmationHeader, "confirmed")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func login(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	response := doRequest(
		t,
		handler,
		http.MethodPost,
		"/api/v1/session",
		`{"username":"admin","password":"a-secure-test-password"}`,
		nil,
		false,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", response.Code, response.Body.String())
	}
	result := response.Result()
	for _, cookie := range result.Cookies() {
		if cookie.Name == sessionCookieName {
			return cookie
		}
	}
	t.Fatal("login did not return a session cookie")
	return nil
}
