package pveweb

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const sessionCookieName = "powercheck_session"

const (
	loginFailureLimit  = 5
	loginBlockDuration = 5 * time.Minute
	loginWindow        = 10 * time.Minute
)

type session struct {
	username string
	expires  time.Time
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[[sha256.Size]byte]session
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginAttempt struct {
	failures     int
	windowStart  time.Time
	blockedUntil time.Time
}

type sessionResponse struct {
	Authenticated   bool      `json:"authenticated"`
	Username        string    `json:"username,omitempty"`
	Node            string    `json:"node,omitempty"`
	ExpiresAt       time.Time `json:"expires_at,omitempty"`
	SecureTransport bool      `json:"secure_transport"`
}

func (s *Server) handleLogin(writer http.ResponseWriter, request *http.Request) {
	input, ok := decodeJSON[loginRequest](writer, request, 4096)
	if !ok {
		return
	}
	remote := remoteHost(request.RemoteAddr)
	if !s.loginAllowed(remote, time.Now()) {
		s.auditLogin(request, false)
		writer.Header().Set("Retry-After", fmt.Sprintf("%.0f", loginBlockDuration.Seconds()))
		writeJSON(writer, http.StatusTooManyRequests, actionResponse{
			Node:  s.Node,
			Error: "登录失败次数过多，请稍后再试",
		})
		return
	}
	userMatch := subtle.ConstantTimeCompare([]byte(input.Username), []byte(s.Username)) == 1
	passwordMatch := verifyPassword(s.PasswordHash, input.Password)
	if !userMatch || !passwordMatch {
		s.recordLoginFailure(remote, time.Now())
		s.auditLogin(request, false)
		writeJSON(writer, http.StatusUnauthorized, actionResponse{
			Node:  s.Node,
			Error: "用户名或密码错误",
		})
		return
	}

	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		s.writeError(writer, http.StatusInternalServerError, fmt.Errorf("create session: %w", err))
		return
	}
	rawToken := base64.RawURLEncoding.EncodeToString(token)
	expires := time.Now().Add(s.sessionTTL())
	s.clearLoginFailures(remote)
	s.sessions.put(rawToken, session{username: s.Username, expires: expires})
	s.setSessionCookie(writer, request, rawToken, expires)
	s.auditLogin(request, true)
	writeJSON(writer, http.StatusOK, sessionResponse{
		Authenticated:   true,
		Username:        s.Username,
		Node:            s.Node,
		ExpiresAt:       expires,
		SecureTransport: secureRequest(request),
	})
}

func (s *Server) handleSession(writer http.ResponseWriter, request *http.Request) {
	current, ok := s.sessionFor(request)
	if !ok {
		writeJSON(writer, http.StatusUnauthorized, sessionResponse{
			Authenticated:   false,
			SecureTransport: secureRequest(request),
		})
		return
	}
	writeJSON(writer, http.StatusOK, sessionResponse{
		Authenticated:   true,
		Username:        current.username,
		Node:            s.Node,
		ExpiresAt:       current.expires,
		SecureTransport: secureRequest(request),
	})
}

func (s *Server) handleLogout(writer http.ResponseWriter, request *http.Request) {
	if cookie, err := request.Cookie(sessionCookieName); err == nil {
		s.sessions.delete(cookie.Value)
	}
	http.SetCookie(writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secureRequest(request),
	})
	writeJSON(writer, http.StatusOK, sessionResponse{
		Authenticated:   false,
		SecureTransport: secureRequest(request),
	})
}

func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if _, ok := s.sessionFor(request); !ok {
			writeJSON(writer, http.StatusUnauthorized, actionResponse{
				Node:  s.Node,
				Error: "登录状态已失效，请重新登录",
			})
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (s *Server) sessionFor(request *http.Request) (session, bool) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil {
		return session{}, false
	}
	return s.sessions.get(cookie.Value, time.Now())
}

func (s *Server) setSessionCookie(
	writer http.ResponseWriter,
	request *http.Request,
	token string,
	expires time.Time,
) {
	http.SetCookie(writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(s.sessionTTL().Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secureRequest(request),
	})
}

func (s *Server) sessionTTL() time.Duration {
	if s.SessionTTL > 0 {
		return s.SessionTTL
	}
	return 12 * time.Hour
}

func (s *Server) auditLogin(request *http.Request, success bool) {
	if s.Logger == nil {
		return
	}
	s.Logger.Printf(
		"action=%q success=%t remote=%q",
		"login",
		success,
		request.RemoteAddr,
	)
}

func secureRequest(request *http.Request) bool {
	return request.TLS != nil ||
		strings.EqualFold(strings.TrimSpace(request.Header.Get("X-Forwarded-Proto")), "https")
}

func remoteHost(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil && host != "" {
		return host
	}
	return address
}

func (s *Server) loginAllowed(remote string, now time.Time) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	attempt, ok := s.logins[remote]
	if !ok {
		return true
	}
	if !attempt.blockedUntil.IsZero() && attempt.blockedUntil.After(now) {
		return false
	}
	if now.Sub(attempt.windowStart) >= loginWindow {
		delete(s.logins, remote)
	}
	return true
}

func (s *Server) recordLoginFailure(remote string, now time.Time) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	attempt := s.logins[remote]
	if attempt.windowStart.IsZero() || now.Sub(attempt.windowStart) >= loginWindow {
		attempt = loginAttempt{windowStart: now}
	}
	attempt.failures++
	if attempt.failures >= loginFailureLimit {
		attempt.blockedUntil = now.Add(loginBlockDuration)
	}
	s.logins[remote] = attempt
}

func (s *Server) clearLoginFailures(remote string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	delete(s.logins, remote)
}

func (s *sessionStore) put(token string, value session) {
	key := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for storedKey, stored := range s.sessions {
		if !stored.expires.After(now) {
			delete(s.sessions, storedKey)
		}
	}
	s.sessions[key] = value
}

func (s *sessionStore) get(token string, now time.Time) (session, bool) {
	if token == "" {
		return session{}, false
	}
	key := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.sessions[key]
	if !ok || !value.expires.After(now) {
		delete(s.sessions, key)
		return session{}, false
	}
	return value, true
}

func (s *sessionStore) delete(token string) {
	key := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, key)
}
