package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/user"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/common/session"
)

// --- helpers ---------------------------------------------------------------

func newRouterForTests(h *Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// minimal routes like your BuildRouter does:
	pub := r.Group("/auth")
	priv := r.Group("/auth")
	priv.Use(h.SM.RequireSession())

	// Bind the same endpoints you use in production:
	pub.POST("/login", h.Login)
	priv.POST("/logout", h.Logout)
	priv.GET("/me", h.Me)

	return r
}

func doJSON(r http.Handler, method, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
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
	oldStart, oldCall, oldGet, oldLookup := startBridge, callBridgeWithSess, getBridgeBinary, lookupUser
	defer func() {
		startBridge, callBridgeWithSess, getBridgeBinary, lookupUser = oldStart, oldCall, oldGet, oldLookup
	}()

	lookupUser = func(username string) (*user.User, error) {
		// fake /etc/passwd lookup
		return &user.User{Username: username, Uid: "1000", Gid: "1000"}, nil
	}
	getBridgeBinary = func(override string) string { return "/fake/bridge" }
	startBridge = func(_ *session.Session, _ string, _ string, _ bool, _ string) (bool, error) {
		return true, nil // privileged
	}
	callBridgeWithSess = func(_ *session.Session, group, cmd string, _ []string) ([]byte, error) {
		if group != "control" || cmd != "ping" {
			t.Fatalf("unexpected bridge call %s.%s", group, cmd)
		}
		return []byte(`{"status":"ok","output":{"type":"pong"}}`), nil
	}

	// Manager + handlers
	sm := session.NewManager(session.New(), session.SessionConfig{})
	h := &Handlers{SM: sm, Env: "development", Verbose: true}
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
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Fatalf("expected success=true, got %v", resp)
	}
	if resp["privileged"] != true {
		t.Fatalf("expected privileged=true, got %v", resp)
	}

	// Me works with cookie
	w2 := doJSON(r, "GET", "/auth/me", nil, c)
	if w2.Code != http.StatusOK {
		t.Fatalf("me: want 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestLogin_AuthFailure_MapsTo401_AndDeletesSession(t *testing.T) {
	oldStart, oldCall, oldLookup := startBridge, callBridgeWithSess, lookupUser
	defer func() { startBridge, callBridgeWithSess, lookupUser = oldStart, oldCall, oldLookup }()

	lookupUser = func(username string) (*user.User, error) {
		return &user.User{Username: username, Uid: "1000", Gid: "1000"}, nil
	}
	startBridge = func(_ *session.Session, _ string, _ string, _ bool, _ string) (bool, error) {
		return false, fmt.Errorf("authentication failed: bad credentials")
	}
	callBridgeWithSess = func(_ *session.Session, _, _ string, _ []string) ([]byte, error) {
		t.Fatal("should not be called when startBridge fails")
		return nil, nil
	}

	sm := session.NewManager(session.New(), session.SessionConfig{})
	h := &Handlers{SM: sm, Env: "development"}
	r := newRouterForTests(h)

	w := doJSON(r, "POST", "/auth/login", LoginRequest{Username: "miguel", Password: "bad"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d: %s", w.Code, w.Body.String())
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

func TestLogin_BridgeStartsButPingFails_MapsTo500_AndSessionRemoved(t *testing.T) {
	oldStart, oldCall, oldLookup := startBridge, callBridgeWithSess, lookupUser
	defer func() { startBridge, callBridgeWithSess, lookupUser = oldStart, oldCall, oldLookup }()

	lookupUser = func(username string) (*user.User, error) {
		return &user.User{Username: username, Uid: "1000", Gid: "1000"}, nil
	}
	startBridge = func(_ *session.Session, _ string, _ string, _ bool, _ string) (bool, error) {
		return false, nil // started ok (non-privileged)
	}
	callBridgeWithSess = func(_ *session.Session, _, _ string, _ []string) ([]byte, error) {
		return nil, fmt.Errorf("socket not ready")
	}

	sm := session.NewManager(session.New(), session.SessionConfig{})
	h := &Handlers{SM: sm, Env: "development"}
	r := newRouterForTests(h)

	w := doJSON(r, "POST", "/auth/login", LoginRequest{Username: "miguel", Password: "pw"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d: %s", w.Code, w.Body.String())
	}
	// No session cookie since we delete the session on failure:
	if ck := w.Result().Cookies(); len(ck) > 0 {
		for _, c := range ck {
			if c.Name == sm.CookieName() {
				t.Fatalf("session cookie should not be set if ping fails, got %v", c)
			}
		}
	}
}

func TestLogout_ClearsCookie_AndDeletesSession(t *testing.T) {
	// Minimal happy path to get a session cookie:
	sm := session.NewManager(session.New(), session.SessionConfig{})
	h := &Handlers{SM: sm, Env: "development"}
	r := newRouterForTests(h)

	// Stub seams for login:
	oldStart, oldCall, oldLookup := startBridge, callBridgeWithSess, lookupUser
	defer func() { startBridge, callBridgeWithSess, lookupUser = oldStart, oldCall, oldLookup }()
	lookupUser = func(username string) (*user.User, error) {
		return &user.User{Username: username, Uid: "1000", Gid: "1000"}, nil
	}
	startBridge = func(_ *session.Session, _ string, _ string, _ bool, _ string) (bool, error) {
		return false, nil
	}
	callBridgeWithSess = func(_ *session.Session, _, _ string, _ []string) ([]byte, error) {
		return []byte(`{"status":"ok","output":{"type":"pong"}}`), nil
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

	// Me should now be 401
	w3 := doJSON(r, "GET", "/auth/me", nil, cookie)
	if w3.Code != http.StatusUnauthorized {
		t.Fatalf("/auth/me after logout should be 401, got %d", w3.Code)
	}
}
