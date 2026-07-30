package managerweb

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"powercheck/internal/nutnetwork"
	"powercheck/internal/upshistory"
	"powercheck/internal/wol"
)

type nutStub struct{}

func (nutStub) Read(context.Context) (nutnetwork.Status, error) {
	charge := 100.0
	load := 38.0
	return nutnetwork.Status{
		Address:        "192.168.1.200:3493",
		UPSName:        "ups",
		Description:    "Synology UPS",
		UPSStatus:      "OL",
		UPSLoadPercent: &load,
		BatteryCharge:  &charge,
	}, nil
}

type wolStub struct {
	wakeID string
}

type historyStub struct{}

func (historyStub) Report(since time.Time, maxPoints int) upshistory.Report {
	load := 55.0
	return upshistory.Report{
		Connected: true,
		Points: []upshistory.Point{{
			CheckedAt:   time.Now(),
			Connected:   true,
			LoadPercent: &load,
		}},
		From: since,
		To:   time.Now(),
	}
}

func (s *wolStub) Status() wol.Status {
	return wol.Status{
		DurationSeconds: 120,
		IntervalSeconds: 30,
		Devices: []wol.DeviceStatus{{
			Device: wol.Device{
				ID:        "pve-one",
				Name:      "PVE One",
				IP:        "192.168.1.66",
				MAC:       "7C:C3:85:BE:65:CC",
				Broadcast: "192.168.1.255",
				Port:      9,
				Enabled:   true,
			},
		}},
	}
}

func (s *wolStub) Wake(deviceID string) (wol.Job, error) {
	s.wakeID = deviceID
	if deviceID != "pve-one" {
		return wol.Job{}, wol.ErrDeviceNotFound
	}
	return wol.Job{
		DeviceID:      deviceID,
		State:         "running",
		TotalAttempts: 4,
		StartedAt:     time.Now(),
	}, nil
}

func TestParseUpstream(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"",
		"ftp://pve.example.test",
		"http://user:password@pve.example.test",
		"http://pve.example.test/api",
		"http://pve.example.test?token=secret",
	} {
		if _, err := ParseUpstream(raw); err == nil {
			t.Fatalf("ParseUpstream(%q) succeeded", raw)
		}
	}
	upstream, err := ParseUpstream("http://192.0.2.10:8765/")
	if err != nil {
		t.Fatalf("ParseUpstream: %v", err)
	}
	if got := upstream.String(); got != "http://192.0.2.10:8765" {
		t.Fatalf("unexpected upstream: %q", got)
	}
}

func TestHandlerServesSPAAndProxiesAPI(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/status" {
			t.Errorf("unexpected upstream path: %s", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		http.SetCookie(writer, &http.Cookie{
			Name:     "powercheck_session",
			Value:    "test-session",
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
		_, _ = io.WriteString(writer, `{"node":"pve"}`)
	}))
	defer upstream.Close()

	webRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("manager index"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(webRoot, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webRoot, "assets", "app.js"), []byte("console.log('ok')"), 0o644); err != nil {
		t.Fatal(err)
	}
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := (&Server{
		Upstream: upstreamURL,
		WebRoot:  webRoot,
		NUT:      nutStub{},
		Logger:   log.New(io.Discard, "", 0),
	}).Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	for _, route := range []string{"/", "/settings", "/assets/app.js"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s returned %d", route, response.Code)
		}
		if response.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatalf("%s missing security headers", route)
		}
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"node":"pve"`) {
		t.Fatalf("unexpected API response: %d %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "powercheck_session" || !cookies[0].HttpOnly {
		t.Fatalf("session cookie was not forwarded: %#v", cookies)
	}

	nutResponse := httptest.NewRecorder()
	handler.ServeHTTP(nutResponse, httptest.NewRequest(http.MethodGet, "/api/manager/v1/nut", nil))
	if nutResponse.Code != http.StatusOK ||
		!strings.Contains(nutResponse.Body.String(), `"connected":true`) ||
		!strings.Contains(nutResponse.Body.String(), `"ups_status":"OL"`) ||
		!strings.Contains(nutResponse.Body.String(), `"ups_load_percent":38`) {
		t.Fatalf("unexpected NUT response: %d %s", nutResponse.Code, nutResponse.Body.String())
	}
}

func TestHandlerRejectsMissingWebRoot(t *testing.T) {
	t.Parallel()
	upstream, err := ParseUpstream("http://192.0.2.10:8765")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&Server{Upstream: upstream, WebRoot: t.TempDir()}).Handler(); err == nil {
		t.Fatal("Handler succeeded without index.html")
	}
}

func TestManagerProxyRejectsPVEShutdownOperations(t *testing.T) {
	t.Parallel()
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		upstreamCalls++
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	webRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("manager index"), 0o644); err != nil {
		t.Fatal(err)
	}
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := (&Server{
		Upstream: upstreamURL,
		WebRoot:  webRoot,
		Logger:   log.New(io.Discard, "", 0),
	}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	for _, route := range []string{
		"/api/v1/guest-shutdown",
		"/api/v1/stopall",
		"/api/v1/host-poweroff",
		"/api/v1/unlisted",
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, route, strings.NewReader(`{}`))
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s returned %d: %s", route, response.Code, response.Body.String())
		}
	}
	if upstreamCalls != 0 {
		t.Fatalf("blocked requests reached PVE upstream %d times", upstreamCalls)
	}
}

func TestWOLAPIRequiresSessionAndExactConfirmation(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/session" {
			t.Fatalf("unexpected upstream path: %s", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		if cookie, err := request.Cookie("powercheck_session"); err != nil || cookie.Value != "valid" {
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(writer, `{"authenticated":false}`)
			return
		}
		_, _ = io.WriteString(writer, `{"authenticated":true}`)
	}))
	defer upstream.Close()

	webRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("manager index"), 0o644); err != nil {
		t.Fatal(err)
	}
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	wake := &wolStub{}
	handler, err := (&Server{
		Upstream:   upstreamURL,
		WebRoot:    webRoot,
		UPSHistory: historyStub{},
		WOL:        wake,
		Logger:     log.New(io.Discard, "", 0),
	}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/manager/v1/wol", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/api/manager/v1/wol", nil)
	statusRequest.AddCookie(&http.Cookie{Name: "powercheck_session", Value: "valid"})
	statusResponse := httptest.NewRecorder()
	handler.ServeHTTP(statusResponse, statusRequest)
	if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), `"pve-one"`) {
		t.Fatalf("status response=%d body=%s", statusResponse.Code, statusResponse.Body.String())
	}

	unconfirmed := httptest.NewRequest(
		http.MethodPost,
		"/api/manager/v1/wol/pve-one/wake",
		strings.NewReader(`{"confirm_device":"pve-one"}`),
	)
	unconfirmed.AddCookie(&http.Cookie{Name: "powercheck_session", Value: "valid"})
	unconfirmed.Header.Set("Content-Type", "application/json")
	unconfirmedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unconfirmedResponse, unconfirmed)
	if unconfirmedResponse.Code != http.StatusPreconditionFailed {
		t.Fatalf("unconfirmed status=%d body=%s", unconfirmedResponse.Code, unconfirmedResponse.Body.String())
	}

	confirmed := httptest.NewRequest(
		http.MethodPost,
		"/api/manager/v1/wol/pve-one/wake",
		strings.NewReader(`{"confirm_device":"pve-one"}`),
	)
	confirmed.AddCookie(&http.Cookie{Name: "powercheck_session", Value: "valid"})
	confirmed.Header.Set("Content-Type", "application/json")
	confirmed.Header.Set("X-PowerCheck-Action", "wake:pve-one")
	confirmedResponse := httptest.NewRecorder()
	handler.ServeHTTP(confirmedResponse, confirmed)
	if confirmedResponse.Code != http.StatusAccepted || wake.wakeID != "pve-one" {
		t.Fatalf("confirmed status=%d body=%s wake=%q", confirmedResponse.Code, confirmedResponse.Body.String(), wake.wakeID)
	}

	historyRequest := httptest.NewRequest(http.MethodGet, "/api/manager/v1/ups-history?hours=24", nil)
	historyRequest.AddCookie(&http.Cookie{Name: "powercheck_session", Value: "valid"})
	historyResponse := httptest.NewRecorder()
	handler.ServeHTTP(historyResponse, historyRequest)
	if historyResponse.Code != http.StatusOK ||
		!strings.Contains(historyResponse.Body.String(), `"load_percent":55`) {
		t.Fatalf("history status=%d body=%s", historyResponse.Code, historyResponse.Body.String())
	}
}
