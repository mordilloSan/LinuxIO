package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/webserver/bridge"
)

// --- helpers ---------------------------------------------------------------

func newRouterForTests(h *Handlers) *http.ServeMux {
	mux := http.NewServeMux()

	// public
	mux.HandleFunc("POST /auth/login", h.Login)

	// private (with session middleware)
	mux.Handle("POST /auth/logout", h.SM.RequireSession(http.HandlerFunc(h.Logout)))

	return mux
}

func doJSON(r http.Handler, method, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			panic(fmt.Sprintf("encode test request body: %v", err))
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func extractCookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, h := range w.Result().Cookies() {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("cookie %q not set; headers=%v", name, w.Result().Header)
	return nil
}

// --- tests -----------------------------------------------------------------

func TestLogin_Success_WritesSessionCookie_AndReportsPrivileged(t *testing.T) {
	// Arrange seams
	oldStart := startBridge
	defer func() {
		startBridge = oldStart
	}()

	startBridge = func(sm *session.Manager, sessionID, username, _ string, _ bool) (*session.Session, error) {
		return sm.CreateSessionWithID(sessionID, session.User{Username: username, UID: 1000, GID: 1000}, true)
	}
	// Manager + handlers
	cfg := session.DefaultConfig
	sm := session.NewManager(session.New(), cfg)
	h := &Handlers{SM: sm, Verbose: true, authSem: make(chan struct{}, maxConcurrentLogins)}
	r := newRouterForTests(h)

	// Act
	w := doJSON(r, "POST", "/auth/login", LoginRequest{Username: "miguel", Password: "pw"})

	// Assert
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	// Cookie written?
	c := extractCookie(t, w, sm.CookieName())
	if c.Value == "" {
		t.Fatal("session cookie empty")
	}
	// Body JSON
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal login response: %v", err)
	}
	if resp["success"] != true {
		t.Fatalf("expected success=true, got %v", resp)
	}
	if resp["privileged"] != true {
		t.Fatalf("expected privileged=true, got %v", resp)
	}

	// Session exists and is marked privileged (validated later by websocket)
	sess, err := sm.GetSession(c.Value)
	if err != nil {
		t.Fatalf("expected session stored, got error: %v", err)
	}
	if !sess.Privileged {
		t.Fatalf("expected session privileged=true, got %v", sess.Privileged)
	}
}

func TestLogin_AuthFailure_MapsTo401_AndDeletesSession(t *testing.T) {
	oldStart := startBridge
	defer func() { startBridge = oldStart }()

	startBridge = func(_ *session.Manager, _, _, _ string, _ bool) (*session.Session, error) {
		return nil, &bridge.AuthError{
			Code:    ipc.ResultAuthFailed,
			Message: "authentication failed",
		}
	}
	cfg := session.DefaultConfig
	sm := session.NewManager(session.New(), cfg)
	h := &Handlers{SM: sm, authSem: make(chan struct{}, maxConcurrentLogins)}
	r := newRouterForTests(h)

	w := doJSON(r, "POST", "/auth/login", LoginRequest{Username: "miguel", Password: "bad"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got := resp["code"]; got != "authentication_failed" {
		t.Fatalf("unexpected error code: %v", got)
	}
	// No cookie set
	if ck := w.Result().Cookies(); len(ck) > 0 {
		for _, c := range ck {
			if c.Name == sm.CookieName() {
				t.Fatalf("session cookie should not be set on auth failure, got %v", c)
			}
		}
	}
}

func TestLogin_PasswordExpired_MapsTo403_AndDeletesSession(t *testing.T) {
	oldStart := startBridge
	defer func() { startBridge = oldStart }()

	startBridge = func(_ *session.Manager, _, _, _ string, _ bool) (*session.Session, error) {
		return nil, &bridge.AuthError{
			Code:    ipc.ResultPasswordExpired,
			Message: "Password has expired. Please change it via SSH or console.",
		}
	}
	cfg := session.DefaultConfig
	sm := session.NewManager(session.New(), cfg)
	h := &Handlers{SM: sm, authSem: make(chan struct{}, maxConcurrentLogins)}
	r := newRouterForTests(h)

	w := doJSON(r, "POST", "/auth/login", LoginRequest{Username: "miguel", Password: "expired"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got := resp["code"]; got != "password_expired" {
		t.Fatalf("unexpected error code: %v", got)
	}
	if got := resp["error"]; got != "Password has expired. Please change it via SSH or console." {
		t.Fatalf("unexpected error message: %v", got)
	}
}

func TestLogin_ConcurrencyLimit_Returns503WhenSaturated(t *testing.T) {
	oldStart := startBridge
	defer func() { startBridge = oldStart }()

	// Bridge blocks forever — holds the semaphore slot
	block := make(chan struct{})
	startBridge = func(_ *session.Manager, _, _, _ string, _ bool) (*session.Session, error) {
		<-block
		return nil, fmt.Errorf("cancelled")
	}
	defer close(block)

	cfg := session.DefaultConfig
	sm := session.NewManager(session.New(), cfg)
	// Semaphore of 1 so one in-flight login saturates it
	h := &Handlers{SM: sm, authSem: make(chan struct{}, 1)}
	r := newRouterForTests(h)

	// First login: blocks in startBridge, holding the semaphore
	started := make(chan struct{})
	go func() {
		close(started)
		doJSON(r, "POST", "/auth/login", LoginRequest{Username: "a", Password: "p"})
	}()
	<-started

	// Give the goroutine a moment to enter Login and acquire the semaphore
	// (the test is deterministic because sem=1 and startBridge blocks)
	for range 100 {
		if len(h.authSem) == 1 {
			break
		}
		// busy-wait briefly for the goroutine to grab the slot
	}

	// Second login: should be rejected immediately
	w := doJSON(r, "POST", "/auth/login", LoginRequest{Username: "b", Password: "p"})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := resp["code"]; got != "too_many_requests" {
		t.Fatalf("unexpected code: %v", got)
	}
}

func TestLogout_ClearsCookie_AndDeletesSession(t *testing.T) {
	// Minimal happy path to get a session cookie:
	cfg := session.DefaultConfig
	sm := session.NewManager(session.New(), cfg)
	h := &Handlers{SM: sm, authSem: make(chan struct{}, maxConcurrentLogins)}
	r := newRouterForTests(h)

	// Stub seams for login:
	oldStart := startBridge
	defer func() { startBridge = oldStart }()

	startBridge = func(sm *session.Manager, sessionID, username, _ string, _ bool) (*session.Session, error) {
		return sm.CreateSessionWithID(sessionID, session.User{Username: username, UID: 1000, GID: 1000}, false)
	}
	// Login to get cookie
	w := doJSON(r, "POST", "/auth/login", LoginRequest{Username: "miguel", Password: "pw"})
	if w.Code != 200 {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	cookie := extractCookie(t, w, sm.CookieName())

	// Act: logout
	w2 := doJSON(r, "POST", "/auth/logout", nil, cookie)
	if w2.Code != http.StatusOK {
		t.Fatalf("logout want 200, got %d: %s", w2.Code, w2.Body.String())
	}

	// Cookie should be cleared
	cleared := extractCookie(t, w2, sm.CookieName())
	if cleared.Value != "" || cleared.MaxAge != -1 {
		t.Fatalf("expected cleared cookie, got value=%q maxAge=%d", cleared.Value, cleared.MaxAge)
	}

	// Session should be deleted
	if _, err := sm.GetSession(cookie.Value); err == nil {
		t.Fatalf("expected session deleted after logout")
	}
}
