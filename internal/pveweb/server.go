package pveweb

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"powercheck/internal/pveexec"
	"powercheck/internal/pvereader"
)

const confirmationHeader = "X-PowerCheck-Action"

type PVEExecutor interface {
	Status(context.Context) (pveexec.Result, error)
	ShutdownGuest(context.Context, int) (pveexec.Result, error)
	StopAll(context.Context) (pveexec.Result, error)
	PoweroffHost(context.Context) (pveexec.Result, error)
}

type AgentTester interface {
	TestAgent(context.Context, int) (pvereader.AgentTestResult, error)
}

type Server struct {
	Node        string
	Executor    PVEExecutor
	Agents      AgentTester
	Username    string
	Password    string
	WebRoot     string
	Logger      *log.Logger
	ActionLimit time.Duration

	actionSlot chan struct{}
}

type statusResponse struct {
	Node   string         `json:"node"`
	Result pveexec.Result `json:"result"`
}

type actionRequest struct {
	VMID            int    `json:"vmid,omitempty"`
	ConfirmVMID     int    `json:"confirm_vmid,omitempty"`
	ConfirmNode     string `json:"confirm_node,omitempty"`
	ConfirmPoweroff string `json:"confirm_poweroff,omitempty"`
}

type actionResponse struct {
	Node   string                    `json:"node"`
	Result pveexec.Result            `json:"result,omitempty"`
	Agent  pvereader.AgentTestResult `json:"agent_result,omitempty"`
	Error  string                    `json:"error,omitempty"`
}

func (s *Server) Handler() (http.Handler, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	s.actionSlot = make(chan struct{}, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	mux.HandleFunc("POST /api/v1/agent-test", s.handleAgentTest)
	mux.HandleFunc("POST /api/v1/guest-shutdown", s.handleGuestShutdown)
	mux.HandleFunc("POST /api/v1/stopall", s.handleStopAll)
	mux.HandleFunc("POST /api/v1/host-poweroff", s.handleHostPoweroff)
	mux.Handle("/", s.staticHandler())

	return s.securityHeaders(s.basicAuth(mux)), nil
}

func (s *Server) ListenAndServe(ctx context.Context, address string) error {
	handler, err := s.Handler()
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      s.actionTimeout() + 15*time.Second,
		IdleTimeout:       60 * time.Second,
	}
	stopped := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		stopped <- err
	}()

	select {
	case err := <-stopped:
		return err
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return err
		}
		return <-stopped
	}
}

func (s *Server) handleStatus(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()
	result, err := s.Executor.Status(ctx)
	if err != nil {
		s.writeError(writer, http.StatusBadGateway, err)
		return
	}
	writeJSON(writer, http.StatusOK, statusResponse{Node: s.Node, Result: result})
}

func (s *Server) handleAgentTest(writer http.ResponseWriter, request *http.Request) {
	input, ok := s.decodeAction(writer, request)
	if !ok {
		return
	}
	if !validVMID(input.VMID) {
		s.writeError(writer, http.StatusBadRequest, fmt.Errorf("invalid VMID"))
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()
	result, err := s.Agents.TestAgent(ctx, input.VMID)
	response := actionResponse{Node: s.Node, Agent: result}
	if err != nil {
		response.Error = err.Error()
		s.audit("agent-test", input.VMID, false, err)
		writeJSON(writer, http.StatusUnprocessableEntity, response)
		return
	}
	s.audit("agent-test", input.VMID, true, nil)
	writeJSON(writer, http.StatusOK, response)
}

func (s *Server) handleGuestShutdown(writer http.ResponseWriter, request *http.Request) {
	input, ok := s.decodeAction(writer, request)
	if !ok {
		return
	}
	if !validVMID(input.VMID) || input.ConfirmVMID != input.VMID {
		s.writeError(writer, http.StatusBadRequest, fmt.Errorf("VMID confirmation does not match"))
		return
	}
	s.runAction(writer, "guest-shutdown", input.VMID, func(ctx context.Context) (pveexec.Result, error) {
		return s.Executor.ShutdownGuest(ctx, input.VMID)
	})
}

func (s *Server) handleStopAll(writer http.ResponseWriter, request *http.Request) {
	input, ok := s.decodeAction(writer, request)
	if !ok {
		return
	}
	if input.ConfirmNode != s.Node {
		s.writeError(writer, http.StatusBadRequest, fmt.Errorf("node confirmation does not match"))
		return
	}
	s.runAction(writer, "stopall", 0, s.Executor.StopAll)
}

func (s *Server) handleHostPoweroff(writer http.ResponseWriter, request *http.Request) {
	input, ok := s.decodeAction(writer, request)
	if !ok {
		return
	}
	if input.ConfirmNode != s.Node ||
		input.ConfirmPoweroff != "POWER OFF "+s.Node {
		s.writeError(writer, http.StatusBadRequest, fmt.Errorf("host poweroff confirmation does not match"))
		return
	}
	s.runAction(writer, "host-poweroff", 0, s.Executor.PoweroffHost)
}

func (s *Server) runAction(
	writer http.ResponseWriter,
	action string,
	vmid int,
	run func(context.Context) (pveexec.Result, error),
) {
	select {
	case s.actionSlot <- struct{}{}:
		defer func() { <-s.actionSlot }()
	default:
		s.writeError(writer, http.StatusConflict, fmt.Errorf("another PVE action is already running"))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.actionTimeout())
	defer cancel()
	result, err := run(ctx)
	response := actionResponse{Node: s.Node, Result: result}
	if err != nil {
		response.Error = err.Error()
		s.audit(action, vmid, false, err)
		writeJSON(writer, http.StatusUnprocessableEntity, response)
		return
	}
	s.audit(action, vmid, true, nil)
	writeJSON(writer, http.StatusOK, response)
}

func (s *Server) decodeAction(writer http.ResponseWriter, request *http.Request) (actionRequest, bool) {
	if request.Header.Get(confirmationHeader) != "confirmed" {
		s.writeError(writer, http.StatusForbidden, fmt.Errorf("missing action confirmation header"))
		return actionRequest{}, false
	}
	if mediaType := request.Header.Get("Content-Type"); !strings.HasPrefix(mediaType, "application/json") {
		s.writeError(writer, http.StatusUnsupportedMediaType, fmt.Errorf("Content-Type must be application/json"))
		return actionRequest{}, false
	}
	reader := http.MaxBytesReader(writer, request.Body, 4096)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var input actionRequest
	if err := decoder.Decode(&input); err != nil {
		s.writeError(writer, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return actionRequest{}, false
	}
	if err := ensureJSONEOF(decoder); err != nil {
		s.writeError(writer, http.StatusBadRequest, err)
		return actionRequest{}, false
	}
	return input, true
}

func (s *Server) basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		username, password, ok := request.BasicAuth()
		userMatch := subtle.ConstantTimeCompare([]byte(username), []byte(s.Username)) == 1
		passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(s.Password)) == 1
		if !ok || !userMatch || !passwordMatch {
			writer.Header().Set("WWW-Authenticate", `Basic realm="PowerCheck PVE", charset="UTF-8"`)
			http.Error(writer, "authentication required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(writer, request)
	})
}

func (s *Server) staticHandler() http.Handler {
	root := http.Dir(s.WebRoot)
	files := http.FileServer(root)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		cleaned := filepath.Clean(strings.TrimPrefix(request.URL.Path, "/"))
		if cleaned == "." {
			cleaned = "index.html"
		}
		fullPath := filepath.Join(s.WebRoot, cleaned)
		if info, err := os.Stat(fullPath); err != nil || info.IsDir() {
			request.URL.Path = "/index.html"
		}
		files.ServeHTTP(writer, request)
	})
}

func (s *Server) validate() error {
	switch {
	case s.Node == "":
		return fmt.Errorf("PVE node is required")
	case s.Executor == nil:
		return fmt.Errorf("PVE executor is required")
	case s.Agents == nil:
		return fmt.Errorf("PVE agent tester is required")
	case s.Username == "":
		return fmt.Errorf("web username is required")
	case len(s.Password) < 12:
		return fmt.Errorf("web password must contain at least 12 characters")
	case s.WebRoot == "":
		return fmt.Errorf("web root is required")
	}
	if info, err := os.Stat(filepath.Join(s.WebRoot, "index.html")); err != nil || info.IsDir() {
		return fmt.Errorf("web root %q does not contain index.html", s.WebRoot)
	}
	return nil
}

func (s *Server) actionTimeout() time.Duration {
	if s.ActionLimit > 0 {
		return s.ActionLimit
	}
	return 4 * time.Minute
}

func (s *Server) audit(action string, vmid int, success bool, err error) {
	if s.Logger == nil {
		return
	}
	fields := []string{
		"action=" + strconv.Quote(action),
		"node=" + strconv.Quote(s.Node),
		"success=" + strconv.FormatBool(success),
	}
	if vmid != 0 {
		fields = append(fields, "vmid="+strconv.Itoa(vmid))
	}
	if err != nil {
		fields = append(fields, "error="+strconv.Quote(err.Error()))
	}
	s.Logger.Println(strings.Join(fields, " "))
}

func (s *Server) writeError(writer http.ResponseWriter, status int, err error) {
	writeJSON(writer, status, actionResponse{Node: s.Node, Error: err.Error()})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values are not allowed")
		}
		return fmt.Errorf("decode request: %w", err)
	}
	return nil
}

func validVMID(vmid int) bool {
	return vmid >= 100 && vmid <= 999999999
}
