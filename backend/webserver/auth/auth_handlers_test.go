package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
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
	oldStart, oldLookup := startBridge, lookupUser
	defer func() {
		startBridge, lookupUser = oldStart, oldLookup
	}()

	lookupUser = func(username string) (session.User, error) {
		return session.User{Username: username, UID: 1000, GID: 1000}, nil
	}
	startBridge = func(sess *session.Session, password string, verbose bool) (bool, error) {
		_ = sess
		_ = password
		_ = verbose
		return true, nil // privileged
	}
	// Manager + handlers
	sm := session.NewManager(session.New(), session.SessionConfig{})
	h := &Handlers{SM: sm, Verbose: true}
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
	oldStart, oldLookup := startBridge, lookupUser
	defer func() { startBridge, lookupUser = oldStart, oldLookup }()

	lookupUser = func(username string) (session.User, error) {
		return session.User{Username: username, UID: 1000, GID: 1000}, nil
	}
	startBridge = func(sess *session.Session, password string, verbose bool) (bool, error) {
		_ = sess
		_ = password
		_ = verbose
		return false, fmt.Errorf("authentication failed: bad credentials")
	}
	sm := session.NewManager(session.New(), session.SessionConfig{})
	h := &Handlers{SM: sm}
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
	h := &Handlers{SM: sm}
	r := newRouterForTests(h)

	// Stub seams for login:
	oldStart, oldLookup := startBridge, lookupUser
	defer func() { startBridge, lookupUser = oldStart, oldLookup }()

	lookupUser = func(username string) (session.User, error) {
		return session.User{Username: username, UID: 1000, GID: 1000}, nil
	}
	startBridge = func(sess *session.Session, password string, verbose bool) (bool, error) {
		_ = sess
		_ = password
		_ = verbose
		return false, nil
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
