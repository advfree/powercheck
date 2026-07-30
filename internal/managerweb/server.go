package managerweb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"powercheck/internal/nutnetwork"
	"powercheck/internal/upshistory"
	"powercheck/internal/wol"
)

type NUTReader interface {
	Read(context.Context) (nutnetwork.Status, error)
}

type WOLController interface {
	Status() wol.Status
	Wake(string) (wol.Job, error)
}

type UPSHistoryReader interface {
	Report(time.Time, int) upshistory.Report
}

type Server struct {
	Upstream   *url.URL
	WebRoot    string
	NUT        NUTReader
	UPSHistory UPSHistoryReader
	WOL        WOLController
	Events     *EventStore
	HTTPClient *http.Client
	Logger     *log.Logger

	eventMu    sync.Mutex
	eventState map[string]string
}

func ParseUpstream(raw string) (*url.URL, error) {
	upstream, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse PVE URL: %w", err)
	}
	switch {
	case upstream.Scheme != "http" && upstream.Scheme != "https":
		return nil, fmt.Errorf("PVE URL scheme must be http or https")
	case upstream.Host == "":
		return nil, fmt.Errorf("PVE URL host is required")
	case upstream.User != nil:
		return nil, fmt.Errorf("PVE URL must not contain credentials")
	case upstream.RawQuery != "" || upstream.Fragment != "":
		return nil, fmt.Errorf("PVE URL must not contain a query or fragment")
	case upstream.Path != "" && upstream.Path != "/":
		return nil, fmt.Errorf("PVE URL must not contain a path")
	}
	upstream.Path = ""
	return upstream, nil
}

func (s *Server) Handler() (http.Handler, error) {
	if s.Upstream == nil {
		return nil, fmt.Errorf("PVE upstream is required")
	}
	if _, err := ParseUpstream(s.Upstream.String()); err != nil {
		return nil, err
	}
	if s.WebRoot == "" {
		return nil, fmt.Errorf("web root is required")
	}
	if info, err := os.Stat(filepath.Join(s.WebRoot, "index.html")); err != nil || info.IsDir() {
		return nil, fmt.Errorf("web root is missing index.html")
	}

	proxy := httputil.NewSingleHostReverseProxy(s.Upstream)
	director := proxy.Director
	proxy.Director = func(request *http.Request) {
		director(request)
		request.Host = s.Upstream.Host
	}
	proxy.ErrorHandler = func(writer http.ResponseWriter, request *http.Request, err error) {
		s.logger().Printf("proxy %s %s: %v", request.Method, request.URL.Path, err)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(writer).Encode(map[string]string{"error": "PVE backend is unavailable"})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/manager/v1/nut", s.handleNUTStatus)
	mux.HandleFunc("GET /api/manager/v1/ups-history", s.handleUPSHistory)
	mux.HandleFunc("GET /api/manager/v1/wol", s.handleWOLStatus)
	mux.HandleFunc("POST /api/manager/v1/wol/{deviceID}/wake", s.handleWOLWake)
	mux.HandleFunc("GET /api/manager/v1/events", s.handleEvents)
	mux.HandleFunc("POST /api/manager/v1/events/scan", s.handleRecordScan)
	mux.Handle("/api/", s.managerPVEProxy(proxy))
	mux.Handle("/", s.staticHandler())
	s.recordEvent(Event{
		Type:   "success",
		Title:  "PowerCheck Manager 已启动",
		Note:   "监测服务和网页控制台已就绪",
		Source: "manager",
	})
	return securityHeaders(mux), nil
}

func (s *Server) managerPVEProxy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !managerPVEAPIAllowed(request.Method, request.URL.Path) {
			writeJSON(writer, http.StatusForbidden, map[string]string{
				"error": "Manager only permits PVE monitoring, Guard events, production configuration, and dry-run simulation",
			})
			return
		}
		captured := &statusResponseWriter{ResponseWriter: writer}
		next.ServeHTTP(captured, request)
		s.observePVERequest(request.Method, request.URL.Path, captured.Status())
	})
}

func managerPVEAPIAllowed(method, requestPath string) bool {
	switch requestPath {
	case "/api/v1/session":
		return method == http.MethodGet || method == http.MethodPost || method == http.MethodDelete
	case "/api/v1/status", "/api/v1/agent-status", "/api/v1/guard-events":
		return method == http.MethodGet
	case "/api/v1/agent-test", "/api/v1/outage-simulation":
		return method == http.MethodPost
	case "/api/v1/outage-config":
		return method == http.MethodGet || method == http.MethodPut
	default:
		return false
	}
}

func (s *Server) handleNUTStatus(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	if s.NUT == nil {
		writer.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"connected": false,
			"error":     "NUT source is not configured",
		})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 6*time.Second)
	defer cancel()
	status, err := s.NUT.Read(ctx)
	if err != nil {
		s.observeNUTStatus(nutnetwork.Status{}, err)
		s.logger().Printf("read NUT status: %v", err)
		writer.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"connected": false,
			"error":     err.Error(),
		})
		return
	}
	s.observeNUTStatus(status, nil)
	_ = json.NewEncoder(writer).Encode(struct {
		Connected bool `json:"connected"`
		nutnetwork.Status
	}{
		Connected: true,
		Status:    status,
	})
}

func (s *Server) handleUPSHistory(writer http.ResponseWriter, request *http.Request) {
	if !s.requireUpstreamSession(writer, request) {
		return
	}
	if s.UPSHistory == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{
			"error": "UPS history is not configured",
		})
		return
	}
	hours := 24
	if raw := request.URL.Query().Get("hours"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 168 {
			writeJSON(writer, http.StatusBadRequest, map[string]string{
				"error": "history hours must be between 1 and 168",
			})
			return
		}
		hours = parsed
	}
	report := s.UPSHistory.Report(time.Now().UTC().Add(-time.Duration(hours)*time.Hour), 720)
	writeJSON(writer, http.StatusOK, report)
}

func (s *Server) handleWOLStatus(writer http.ResponseWriter, request *http.Request) {
	if !s.requireUpstreamSession(writer, request) {
		return
	}
	if s.WOL == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]any{
			"enabled": false,
			"error":   "WOL is not configured",
		})
		return
	}
	status := s.WOL.Status()
	s.observeWOLStatus(status)
	writeJSON(writer, http.StatusOK, struct {
		Enabled bool `json:"enabled"`
		wol.Status
	}{
		Enabled: true,
		Status:  status,
	})
}

func (s *Server) handleWOLWake(writer http.ResponseWriter, request *http.Request) {
	if !s.requireUpstreamSession(writer, request) {
		return
	}
	if s.WOL == nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{
			"error": "WOL is not configured",
		})
		return
	}
	deviceID := request.PathValue("deviceID")
	if request.Header.Get("X-PowerCheck-Action") != "wake:"+deviceID {
		writeJSON(writer, http.StatusPreconditionFailed, map[string]string{
			"error": "exact WOL confirmation header is required",
		})
		return
	}
	var input struct {
		ConfirmDevice string `json:"confirm_device"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid WOL request"})
		return
	}
	if input.ConfirmDevice != deviceID {
		writeJSON(writer, http.StatusPreconditionFailed, map[string]string{
			"error": "WOL device confirmation does not match",
		})
		return
	}
	job, err := s.WOL.Wake(deviceID)
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, wol.ErrDeviceNotFound):
			status = http.StatusNotFound
		case errors.Is(err, wol.ErrJobRunning):
			status = http.StatusConflict
		}
		writeJSON(writer, status, map[string]string{"error": err.Error()})
		return
	}
	s.logger().Printf("WOL job started for configured device %s", deviceID)
	s.setEventState("wol:"+deviceID, "running")
	s.recordEvent(Event{
		Type:   "info",
		Title:  "WOL 任务已启动：" + deviceID,
		Note:   fmt.Sprintf("计划发送 %d 次 Magic Packet", job.TotalAttempts),
		Source: "wol",
	})
	writeJSON(writer, http.StatusAccepted, map[string]any{
		"accepted": true,
		"job":      job,
	})
}

func (s *Server) handleEvents(writer http.ResponseWriter, request *http.Request) {
	if !s.requireUpstreamSession(writer, request) {
		return
	}
	limit := 50
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			writeJSON(writer, http.StatusBadRequest, map[string]string{
				"error": "event limit must be between 1 and 200",
			})
			return
		}
		limit = parsed
	}
	events := []Event{}
	if s.Events != nil {
		events = s.Events.List(limit)
	}
	events = append(events, s.readGuardEvents(request, limit)...)
	sort.Slice(events, func(i, j int) bool {
		return events[i].CreatedAt.After(events[j].CreatedAt)
	})
	if len(events) > limit {
		events = events[:limit]
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"retention_hours": 24,
		"events":          events,
	})
}

func (s *Server) readGuardEvents(request *http.Request, limit int) []Event {
	eventURL := *s.Upstream
	eventURL.Path = "/api/v1/guard-events"
	query := eventURL.Query()
	query.Set("limit", strconv.Itoa(limit))
	eventURL.RawQuery = query.Encode()
	probe, err := http.NewRequestWithContext(request.Context(), http.MethodGet, eventURL.String(), nil)
	if err != nil {
		return nil
	}
	if cookie := request.Header.Get("Cookie"); cookie != "" {
		probe.Header.Set("Cookie", cookie)
	}
	client := s.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	response, err := client.Do(probe)
	if err != nil {
		s.logger().Printf("read PVE guard events: %v", err)
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil
	}
	var payload struct {
		Events []Event `json:"events"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(&payload); err != nil {
		s.logger().Printf("decode PVE guard events: %v", err)
		return nil
	}
	return payload.Events
}

func (s *Server) handleRecordScan(writer http.ResponseWriter, request *http.Request) {
	if !s.requireUpstreamSession(writer, request) {
		return
	}
	if request.Header.Get("X-PowerCheck-Action") != "record-scan" {
		writeJSON(writer, http.StatusPreconditionFailed, map[string]string{
			"error": "exact scan confirmation header is required",
		})
		return
	}
	var input struct {
		Success bool `json:"success"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid scan result"})
		return
	}
	event := Event{
		Type:   "success",
		Title:  "实时状态检查完成",
		Note:   "已执行 NUT、PVE Guest 与 Guest Agent 只读检查",
		Source: "manager",
	}
	if !input.Success {
		event.Type = "warning"
		event.Title = "实时状态检查部分失败"
		event.Note = "NUT、PVE Guest 或 Guest Agent 状态未能完整读取"
	}
	s.recordEvent(event)
	writeJSON(writer, http.StatusCreated, map[string]bool{"recorded": true})
}

func (s *Server) requireUpstreamSession(writer http.ResponseWriter, request *http.Request) bool {
	probeURL := *s.Upstream
	probeURL.Path = "/api/v1/session"
	probe, err := http.NewRequestWithContext(request.Context(), http.MethodGet, probeURL.String(), nil)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "create session check"})
		return false
	}
	if cookie := request.Header.Get("Cookie"); cookie != "" {
		probe.Header.Set("Cookie", cookie)
	}
	client := s.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	response, err := client.Do(probe)
	if err != nil {
		s.logger().Printf("check PVE session: %v", err)
		writeJSON(writer, http.StatusBadGateway, map[string]string{
			"error": "PVE authentication backend is unavailable",
		})
		return false
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{
			"error": "登录状态已失效，请重新登录",
		})
		return false
	}
	if response.StatusCode != http.StatusOK {
		writeJSON(writer, http.StatusBadGateway, map[string]string{
			"error": "PVE authentication backend rejected the session check",
		})
		return false
	}
	var session struct {
		Authenticated bool `json:"authenticated"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&session); err != nil || !session.Authenticated {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{
			"error": "登录状态已失效，请重新登录",
		})
		return false
	}
	return true
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
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}
	errChannel := make(chan error, 1)
	go func() {
		errChannel <- server.ListenAndServe()
	}()
	select {
	case err := <-errChannel:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	}
}

func (s *Server) staticHandler() http.Handler {
	root := os.DirFS(s.WebRoot)
	files := http.FileServer(http.FS(root))
	indexPath := filepath.Join(s.WebRoot, "index.html")
	index, indexErr := os.ReadFile(indexPath)
	indexInfo, statErr := os.Stat(indexPath)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		filePath := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if filePath == "." || filePath == "" {
			filePath = "index.html"
		}
		if info, err := fs.Stat(root, filePath); err != nil || info.IsDir() {
			filePath = "index.html"
		}
		if filePath == "index.html" {
			if indexErr != nil || statErr != nil {
				http.Error(writer, "web console is unavailable", http.StatusInternalServerError)
				return
			}
			writer.Header().Set("Cache-Control", "no-store")
			http.ServeContent(writer, request, "index.html", indexInfo.ModTime(), bytes.NewReader(index))
			return
		}
		cloned := request.Clone(request.Context())
		cloned.URL.Path = "/" + filePath
		files.ServeHTTP(writer, cloned)
	})
}

func (s *Server) logger() *log.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return log.Default()
}

func (s *Server) observeNUTStatus(status nutnetwork.Status, readErr error) {
	state := "unavailable"
	event := Event{
		Type:   "warning",
		Title:  "UPS 状态读取失败",
		Note:   "无法读取 Synology NUT 状态",
		Source: "nut",
	}
	if readErr == nil {
		state = strings.Join(strings.Fields(status.UPSStatus), " ")
		event = Event{
			Type:   "info",
			Title:  "UPS 状态已更新：" + state,
			Note:   describeNUTStatus(status),
			Source: "nut",
		}
		switch {
		case hasNUTStatus(status.UPSStatus, "OB"), hasNUTStatus(status.UPSStatus, "LB"):
			event.Type = "warning"
			event.Title = "UPS 已进入电池供电：" + state
		case hasNUTStatus(status.UPSStatus, "OL"):
			event.Type = "success"
			event.Title = "UPS 市电状态正常：" + state
		}
	}
	previous, changed := s.transitionEventState("nut", state)
	if !changed {
		return
	}
	if previous != "" && hasNUTStatus(state, "OL") {
		event.Title = "UPS 市电已恢复：" + state
	}
	s.recordEvent(event)
}

func (s *Server) observeWOLStatus(status wol.Status) {
	for _, device := range status.Devices {
		if device.Job == nil {
			continue
		}
		key := "wol:" + device.ID
		_, changed := s.transitionEventState(key, device.Job.State)
		if !changed || device.Job.State == "running" {
			continue
		}
		event := Event{
			Type:   "success",
			Title:  "WOL 发送完成：" + device.Name,
			Note:   fmt.Sprintf("已发送 %d/%d 个 Magic Packet", device.Job.PacketsSent, device.Job.TotalAttempts),
			Source: "wol",
		}
		if device.Job.State != "completed" {
			event.Type = "warning"
			event.Title = "WOL 发送未完成：" + device.Name
			event.Note = device.Job.LastError
			if event.Note == "" {
				event.Note = "任务状态：" + device.Job.State
			}
		}
		s.recordEvent(event)
	}
}

func (s *Server) observePVERequest(method, requestPath string, status int) {
	success := status >= 200 && status < 300
	if method == http.MethodGet && requestPath == "/api/v1/status" {
		state := "unavailable"
		if success {
			state = "online"
		}
		previous, changed := s.transitionEventState("pve", state)
		if !changed {
			return
		}
		event := Event{
			Type:   "success",
			Title:  "PVE 状态读取正常",
			Note:   "Dell P7920 API-only 后端在线",
			Source: "pve",
		}
		if !success {
			event.Type = "warning"
			event.Title = "PVE 状态读取失败"
			event.Note = fmt.Sprintf("Dell P7920 API 返回 HTTP %d", status)
		} else if previous == "unavailable" {
			event.Title = "PVE 状态读取已恢复"
		}
		s.recordEvent(event)
		return
	}

	var title, failureTitle, note string
	switch {
	case method == http.MethodPost && requestPath == "/api/v1/session":
		title = "管理员登录成功"
		failureTitle = "管理员登录失败"
		note = "PVE 服务端会话已建立"
	case method == http.MethodDelete && requestPath == "/api/v1/session":
		title = "管理员已退出"
		failureTitle = "管理员退出失败"
		note = "PVE 服务端会话已结束"
	case method == http.MethodGet && requestPath == "/api/v1/agent-status":
		title = "Guest Agent 检测完成"
		note = "已通过 PVE 实际读取运行中 QEMU VM 的 Agent 状态"
	case method == http.MethodPost && requestPath == "/api/v1/agent-test":
		title = "单个 Guest Agent 测试完成"
		note = "PVE 已执行无损 Agent ping"
	case method == http.MethodPut && requestPath == "/api/v1/outage-config":
		title = "断电响应参数已保存"
		note = "参数已写入 Dell P7920，本操作未发送关机命令"
	case method == http.MethodPost && requestPath == "/api/v1/outage-simulation":
		title = "NUT 断电模拟完成（DRY-RUN）"
		note = "PVE 只生成 WOULD RUN 时间线，未执行系统命令"
	default:
		return
	}
	eventType := "success"
	if !success {
		eventType = "warning"
		if failureTitle != "" {
			title = failureTitle
		} else {
			title += "失败"
		}
		note = fmt.Sprintf("PVE API 返回 HTTP %d", status)
	}
	s.recordEvent(Event{
		Type:   eventType,
		Title:  title,
		Note:   note,
		Source: "pve",
	})
}

func (s *Server) recordEvent(event Event) {
	if s.Events == nil {
		return
	}
	if _, err := s.Events.Add(event); err != nil {
		s.logger().Printf("persist event %q: %v", event.Title, err)
	}
}

func (s *Server) transitionEventState(key, value string) (string, bool) {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	if s.eventState == nil {
		s.eventState = make(map[string]string)
	}
	previous, exists := s.eventState[key]
	if exists && previous == value {
		return previous, false
	}
	s.eventState[key] = value
	return previous, true
}

func (s *Server) setEventState(key, value string) {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	if s.eventState == nil {
		s.eventState = make(map[string]string)
	}
	s.eventState[key] = value
}

func hasNUTStatus(status, target string) bool {
	for _, field := range strings.Fields(status) {
		if field == target {
			return true
		}
	}
	return false
}

func describeNUTStatus(status nutnetwork.Status) string {
	parts := []string{"Synology NUT " + status.Address}
	if status.UPSLoadPercent != nil {
		parts = append(parts, fmt.Sprintf("负载 %.0f%%", *status.UPSLoadPercent))
	}
	if status.BatteryCharge != nil {
		parts = append(parts, fmt.Sprintf("电量 %.0f%%", *status.BatteryCharge))
	}
	if status.BatteryRuntimeSeconds != nil {
		parts = append(parts, fmt.Sprintf("预计续航 %d 秒", *status.BatteryRuntimeSeconds))
	}
	return strings.Join(parts, " · ")
}

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusResponseWriter) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *statusResponseWriter) Write(body []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	return writer.ResponseWriter.Write(body)
}

func (writer *statusResponseWriter) Status() int {
	if writer.status == 0 {
		return http.StatusOK
	}
	return writer.status
}

func (writer *statusResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; font-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(writer, request)
	})
}
