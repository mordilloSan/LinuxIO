package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/auth"
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
	mux.Handle("GET /api/update-info", h.SM.RequireSession(http.HandlerFunc(h.UpdateInfo)))

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

func assertResponseFields(t *testing.T, resp map[string]any, fields map[string]any) {
	t.Helper()
	for key, want := range fields {
		if got := resp[key]; got != want {
			t.Fatalf("expected %s=%v, got %v", key, want, resp)
		}
	}
}

var capabilityJSONKeys = []string{
	"docker_available", "docker_updates_available", "indexer_available", "monitoring_available",
	"lm_sensors_available", "memory_inventory_available", "smartmontools_available",
	"packagekit_available", "nfs_client_available", "nfs_server_available", "samba_server_available",
	"samba_client_available", "tuned_available", "avahi_available", "wireguard_available", "libvirt_available",
}

func assertLoginOmitsCapabilities(t *testing.T, resp map[string]any) {
	t.Helper()
	for _, key := range capabilityJSONKeys {
		if _, ok := resp[key]; ok {
			t.Fatalf("login response unexpectedly contains capability field %q", key)
		}
	}
}

func sessionCookie(t *testing.T, sm *session.Manager, privileged bool) *http.Cookie {
	t.Helper()
	id, err := sm.NewSessionID()
	if err != nil {
		t.Fatalf("new session ID: %v", err)
	}
	if _, err := sm.CreateSession(id, session.User{Username: "test", UID: 1000, GID: 1000}, privileged); err != nil {
		t.Fatalf("create session: %v", err)
	}
	w := httptest.NewRecorder()
	sm.WriteCookie(w, id)
	return extractCookie(t, w, sm.CookieName())
}

// --- tests -----------------------------------------------------------------

func TestLoginRejectsNonCanonicalJSON(t *testing.T) {
	oldStart := startBridge
	t.Cleanup(func() { startBridge = oldStart })

	sm := session.NewManager(session.NewWithCleanupInterval(0), session.DefaultConfig)
	t.Cleanup(sm.Close)
	handler := newRouterForTests(&Handlers{SM: sm, authSem: make(chan struct{}, maxConcurrentLogins)})
	tests := []struct {
		name string
		body []byte
	}{
		{name: "unknown member", body: []byte(`{"username":"user","password":"pw","extra":true}`)},
		{name: "case-mismatched member", body: []byte(`{"Username":"user","password":"pw"}`)},
		{name: "duplicate member", body: []byte(`{"username":"user","username":"other","password":"pw"}`)},
		{name: "invalid UTF-8", body: []byte("{\"username\":\"\xff\",\"password\":\"pw\"}")},
		{name: "trailing value", body: []byte(`{"username":"user","password":"pw"} {}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bridgeStarted := false
			startBridge = func(*session.Manager, string, string, string, string, bool) (*session.Session, error) {
				bridgeStarted = true
				return nil, fmt.Errorf("unexpected bridge start")
			}

			req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusBadRequest, w.Body.String())
			}
			if bridgeStarted {
				t.Fatal("invalid login payload started the bridge")
			}
			var response loginErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.Code != "invalid_request" {
				t.Fatalf("error code = %q, want invalid_request", response.Code)
			}
		})
	}
}

func TestLogin_Success_WritesSessionCookie_AndReportsPrivileged(t *testing.T) {
	// Arrange seams
	oldStart := startBridge
	defer func() {
		startBridge = oldStart
	}()

	var gotRemoteHost string
	startBridge = func(sm *session.Manager, sessionID, username, _, remoteHost string, _ bool) (*session.Session, error) {
		gotRemoteHost = remoteHost
		sess, err := sm.CreateSession(sessionID, session.User{Username: username, UID: 1000, GID: 1000}, true)
		if err != nil {
			return nil, err
		}
		return sess, nil
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
	assertResponseFields(t, resp, map[string]any{
		"success":    true,
		"privileged": true,
	})
	assertLoginOmitsCapabilities(t, resp)
	if _, ok := resp["update"]; ok {
		t.Fatalf("login response unexpectedly contains update information: %v", resp["update"])
	}

	// Session exists and is marked privileged (validated later by websocket)
	sess, err := sm.GetSession(c.Value)
	if err != nil {
		t.Fatalf("expected session stored, got error: %v", err)
	}
	if !sess.Privileged {
		t.Fatalf("expected session privileged=true, got %v", sess.Privileged)
	}
	if gotRemoteHost != "192.0.2.1" {
		t.Fatalf("remote host = %q, want %q", gotRemoteHost, "192.0.2.1")
	}
}

func TestUpdateInfo_MissingSessionReturns401ThroughMiddleware(t *testing.T) {
	sm := session.NewManager(session.NewWithCleanupInterval(0), session.DefaultConfig)
	t.Cleanup(sm.Close)
	mux := http.NewServeMux()
	RegisterAuthRoutes(mux, sm, false)

	w := doJSON(mux, http.MethodGet, "/api/update-info", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateInfo_UnprivilegedReturns204WithoutChecking(t *testing.T) {
	oldCheck := checkForUpdate
	t.Cleanup(func() { checkForUpdate = oldCheck })
	called := false
	checkForUpdate = func(context.Context) *UpdateInfo {
		called = true
		return &UpdateInfo{Available: true}
	}

	sm := session.NewManager(session.NewWithCleanupInterval(0), session.DefaultConfig)
	t.Cleanup(sm.Close)
	h := &Handlers{SM: sm, authSem: make(chan struct{}, maxConcurrentLogins)}
	w := doJSON(newRouterForTests(h), http.MethodGet, "/api/update-info", nil, sessionCookie(t, sm, false))
	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if called {
		t.Fatal("unprivileged update request invoked checkForUpdate")
	}
	if w.Body.Len() != 0 {
		t.Fatalf("want empty 204 body, got %q", w.Body.String())
	}
}

func TestUpdateInfo_PrivilegedNilReturns204(t *testing.T) {
	oldCheck := checkForUpdate
	t.Cleanup(func() { checkForUpdate = oldCheck })
	called := false
	checkForUpdate = func(ctx context.Context) *UpdateInfo {
		called = true
		if ctx == nil {
			t.Fatal("checkForUpdate received nil context")
		}
		return nil
	}

	sm := session.NewManager(session.NewWithCleanupInterval(0), session.DefaultConfig)
	t.Cleanup(sm.Close)
	h := &Handlers{SM: sm, authSem: make(chan struct{}, maxConcurrentLogins)}
	w := doJSON(newRouterForTests(h), http.MethodGet, "/api/update-info", nil, sessionCookie(t, sm, true))
	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("privileged update request did not invoke checkForUpdate")
	}
	if w.Body.Len() != 0 {
		t.Fatalf("want empty 204 body, got %q", w.Body.String())
	}
}

func TestUpdateInfo_PrivilegedResultReturnsExactJSON(t *testing.T) {
	oldCheck := checkForUpdate
	t.Cleanup(func() { checkForUpdate = oldCheck })
	want := &UpdateInfo{
		Available:      true,
		CurrentVersion: "v1.2.3",
		LatestVersion:  "v1.3.0",
		ReleaseURL:     "https://example.test/linuxio/releases/v1.3.0",
	}
	checkForUpdate = func(context.Context) *UpdateInfo { return want }

	sm := session.NewManager(session.NewWithCleanupInterval(0), session.DefaultConfig)
	t.Cleanup(sm.Close)
	h := &Handlers{SM: sm, authSem: make(chan struct{}, maxConcurrentLogins)}
	w := doJSON(newRouterForTests(h), http.MethodGet, "/api/update-info", nil, sessionCookie(t, sm, true))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var got UpdateInfo
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if got != *want {
		t.Fatalf("update response = %+v, want %+v", got, *want)
	}
}

func TestClientRemoteHost_UsesForwardedForFromTrustedProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "172.18.0.4:49832"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 172.18.0.4")

	if got := clientRemoteHost(req); got != "203.0.113.9" {
		t.Fatalf("remote host = %q, want %q", got, "203.0.113.9")
	}
}

func TestClientRemoteHost_UsesRightmostUntrustedForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "127.0.0.1:49832"
	req.Header.Set("X-Forwarded-For", "127.0.0.1, 192.168.1.239")

	if got := clientRemoteHost(req); got != "192.168.1.239" {
		t.Fatalf("remote host = %q, want %q", got, "192.168.1.239")
	}
}

func TestClientRemoteHost_FallsBackToRealIPWhenForwardedForOnlyHasTrustedProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "127.0.0.1:49832"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("X-Real-IP", "192.168.1.239")

	if got := clientRemoteHost(req); got != "192.168.1.239" {
		t.Fatalf("remote host = %q, want %q", got, "192.168.1.239")
	}
}

func TestClientRemoteHost_IgnoresForwardedForFromUntrustedPeer(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "203.0.113.10:49832"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")

	if got := clientRemoteHost(req); got != "203.0.113.10" {
		t.Fatalf("remote host = %q, want %q", got, "203.0.113.10")
	}
}

func TestClientRemoteHost_UsesCustomTrustedProxyCIDR(t *testing.T) {
	t.Setenv(trustedProxyCIDRsEnv, "10.10.0.0/16")

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "10.10.1.20:49832"
	req.Header.Set("X-Real-IP", "198.51.100.7")

	if got := clientRemoteHost(req); got != "198.51.100.7" {
		t.Fatalf("remote host = %q, want %q", got, "198.51.100.7")
	}
}

func TestClientRemoteHost_ParsesRFCForwardedHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "172.18.0.4:49832"
	req.Header.Set("Forwarded", `for="[2001:db8::1]:443";proto=https`)

	if got := clientRemoteHost(req); got != "2001:db8::1" {
		t.Fatalf("remote host = %q, want %q", got, "2001:db8::1")
	}
}

func TestClientRemoteHost_FallsBackToForwardedWhenEarlierHeadersOnlyHaveTrustedProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "127.0.0.1:49832"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("X-Real-IP", "127.0.0.1")
	req.Header.Set("Forwarded", `for=127.0.0.1, for=192.168.1.239`)

	if got := clientRemoteHost(req); got != "192.168.1.239" {
		t.Fatalf("remote host = %q, want %q", got, "192.168.1.239")
	}
}

func TestLogin_Success_OmitsCapabilities(t *testing.T) {
	oldStart := startBridge
	defer func() { startBridge = oldStart }()

	startBridge = func(sm *session.Manager, sessionID, username, _, _ string, _ bool) (*session.Session, error) {
		sess, err := sm.CreateSession(sessionID, session.User{Username: username, UID: 1000, GID: 1000}, false)
		if err != nil {
			return nil, err
		}
		return sess, nil
	}

	cfg := session.DefaultConfig
	sm := session.NewManager(session.New(), cfg)
	h := &Handlers{SM: sm, authSem: make(chan struct{}, maxConcurrentLogins)}
	r := newRouterForTests(h)

	w := doJSON(r, "POST", "/auth/login", LoginRequest{Username: "miguel", Password: "pw"})
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal login response: %v", err)
	}
	assertResponseFields(t, resp, map[string]any{"success": true, "privileged": false})
	assertLoginOmitsCapabilities(t, resp)
}

func TestLogin_AuthFailure_MapsTo401_AndDeletesSession(t *testing.T) {
	oldStart := startBridge
	defer func() { startBridge = oldStart }()

	startBridge = func(*session.Manager, string, string, string, string, bool) (*session.Session, error) {
		return nil, &bridge.AuthError{
			Code:    authipc.ResultAuthFailed,
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

	startBridge = func(*session.Manager, string, string, string, string, bool) (*session.Session, error) {
		return nil, &bridge.AuthError{
			Code:    authipc.ResultPasswordExpired,
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

	// Bridge blocks until released — the first login holds the semaphore slot
	// for the duration of the test. entered signals that startBridge has been
	// reached, which (per Login) only happens after the semaphore is acquired.
	block := make(chan struct{})
	entered := make(chan struct{}, 1)
	startBridge = func(*session.Manager, string, string, string, string, bool) (*session.Session, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
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
	go func() {
		doJSON(r, "POST", "/auth/login", LoginRequest{Username: "a", Password: "p"})
	}()

	// Wait until the first login is inside startBridge; at that point the
	// semaphore is held, so a concurrent login must be rejected.
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first login did not reach startBridge in time")
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

	startBridge = func(sm *session.Manager, sessionID, username, _, _ string, _ bool) (*session.Session, error) {
		return sm.CreateSession(sessionID, session.User{Username: username, UID: 1000, GID: 1000}, false)
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
