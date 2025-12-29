package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/config"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// --- helpers ---------------------------------------------------------------

func newRouterForTests(h *Handlers) *http.ServeMux {
	mux := http.NewServeMux()

	// public
	mux.HandleFunc("POST /auth/login", h.Login)

	// private (with session middleware)
	mux.Handle("POST /auth/logout", h.SM.RequireSession(http.HandlerFunc(h.Logout)))
	mux.Handle("GET /auth/me", h.SM.RequireSession(http.HandlerFunc(h.Me)))

	return mux
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
	oldStart, oldGet, oldLookup := startBridge, getBridgeBinary, lookupUser
	defer func() {
		startBridge, getBridgeBinary, lookupUser = oldStart, oldGet, oldLookup
	}()

	lookupUser = func(username string) (session.User, error) {
		return session.User{Username: username, UID: 1000, GID: 1000}, nil
	}
	getBridgeBinary = func(override string) string {
		_ = override
		return "/fake/bridge"
	}
	startBridge = func(sess *session.Session, password, env string, verbose bool, bin string) (bool, string, error) {
		_ = sess
		_ = password
		_ = env
		_ = verbose
		_ = bin
		return true, "Welcome to LinuxIO!", nil // privileged
	}
	// Manager + handlers
	sm := session.NewManager(session.New(), session.SessionConfig{})
	h := &Handlers{SM: sm, Env: config.EnvDevelopment, Verbose: true}
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
	oldStart, oldLookup := startBridge, lookupUser
	defer func() { startBridge, lookupUser = oldStart, oldLookup }()

	lookupUser = func(username string) (session.User, error) {
		return session.User{Username: username, UID: 1000, GID: 1000}, nil
	}
	startBridge = func(sess *session.Session, password, env string, verbose bool, bin string) (bool, string, error) {
		_ = sess
		_ = password
		_ = env
		_ = verbose
		_ = bin
		return false, "", fmt.Errorf("authentication failed: bad credentials")
	}
	sm := session.NewManager(session.New(), session.SessionConfig{})
	h := &Handlers{SM: sm, Env: config.EnvDevelopment}
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

func TestLogout_ClearsCookie_AndDeletesSession(t *testing.T) {
	// Minimal happy path to get a session cookie:
	sm := session.NewManager(session.New(), session.SessionConfig{})
	h := &Handlers{SM: sm, Env: config.EnvDevelopment}
	r := newRouterForTests(h)

	// Stub seams for login:
	oldStart, oldLookup := startBridge, lookupUser
	defer func() { startBridge, lookupUser = oldStart, oldLookup }()

	lookupUser = func(username string) (session.User, error) {
		return session.User{Username: username, UID: 1000, GID: 1000}, nil
	}
	startBridge = func(sess *session.Session, password, env string, verbose bool, bin string) (bool, string, error) {
		_ = sess
		_ = password
		_ = env
		_ = verbose
		_ = bin
		return false, "", nil
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
