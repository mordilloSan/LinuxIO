package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

func newTestSessionManager(cfg session.SessionConfig) *session.Manager {
	cfg.Cookie.Secure = false
	cfg.GCInterval = 0
	return session.NewManager(session.New(), cfg)
}

func wsURL(httpURL string, path string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + path
}

func TestWebSocketAuthMiddlewareRejectsMissingSessionWithPolicyViolation(t *testing.T) {
	sm := newTestSessionManager(session.DefaultConfig)
	defer sm.Close()

	nextCalled := false
	server := httptest.NewServer(wsAuthMiddleware(sm, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		t.Fatal("next handler should not be called without a session")
	})))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL, "/ws"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_, _, err = conn.ReadMessage()
	closeErr, ok := err.(*websocket.CloseError)
	if !ok {
		t.Fatalf("expected websocket close error, got %T %v", err, err)
	}
	if closeErr.Code != websocket.ClosePolicyViolation {
		t.Fatalf("close code = %d, want %d", closeErr.Code, websocket.ClosePolicyViolation)
	}
	if closeErr.Text != "no-session" {
		t.Fatalf("close text = %q, want no-session", closeErr.Text)
	}
	if nextCalled {
		t.Fatal("next handler was called without a session")
	}
}

func TestWebSocketAuthMiddlewareRejectsExpiredSessionCookieWithPolicyViolation(t *testing.T) {
	cfg := session.DefaultConfig
	cfg.IdleTimeout = -time.Second
	sm := newTestSessionManager(cfg)
	defer sm.Close()

	sess, err := sm.CreateSessionWithID("expired-session", session.User{Username: "miguel", UID: 1000, GID: 1000}, false)
	if err != nil {
		t.Fatalf("create expired session: %v", err)
	}

	server := httptest.NewServer(wsAuthMiddleware(sm, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called for expired session")
	})))
	defer server.Close()

	header := http.Header{}
	header.Add("Cookie", (&http.Cookie{Name: sm.CookieName(), Value: sess.SessionID}).String())
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL, "/ws"), header)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_, _, err = conn.ReadMessage()
	closeErr, ok := err.(*websocket.CloseError)
	if !ok {
		t.Fatalf("expected websocket close error, got %T %v", err, err)
	}
	if closeErr.Code != websocket.ClosePolicyViolation {
		t.Fatalf("close code = %d, want %d", closeErr.Code, websocket.ClosePolicyViolation)
	}
	if closeErr.Text != "no-session" {
		t.Fatalf("close text = %q, want no-session", closeErr.Text)
	}
	if _, err := sm.GetSession(sess.SessionID); err == nil {
		t.Fatal("expired session should be deleted during websocket validation")
	}
}

func TestCloseWebSocketForSessionSendsSessionExpiredPolicyViolation(t *testing.T) {
	wsConnsBySession = sync.Map{}
	defer func() { wsConnsBySession = sync.Map{} }()

	ready := make(chan struct{})
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		addWebSocketForSession("session-1", conn)
		close(ready)
		<-done
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL, "/ws"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("websocket was not registered in time")
	}

	CloseWebSocketForSession("session-1")
	close(done)

	_, _, err = conn.ReadMessage()
	closeErr, ok := err.(*websocket.CloseError)
	if !ok {
		t.Fatalf("expected websocket close error, got %T %v", err, err)
	}
	if closeErr.Code != websocket.ClosePolicyViolation {
		t.Fatalf("close code = %d, want %d", closeErr.Code, websocket.ClosePolicyViolation)
	}
	if closeErr.Text != "Session expired" {
		t.Fatalf("close text = %q, want Session expired", closeErr.Text)
	}
	if _, ok := wsConnsBySession.Load("session-1"); ok {
		t.Fatal("session websocket registry entry should be removed")
	}
}

func TestProtectedRouteReturnsUnauthorizedForExpiredSessionCookie(t *testing.T) {
	cfg := session.DefaultConfig
	cfg.IdleTimeout = -time.Second
	sm := newTestSessionManager(cfg)
	defer sm.Close()

	sess, err := sm.CreateSessionWithID("expired-session", session.User{Username: "miguel", UID: 1000, GID: 1000}, false)
	if err != nil {
		t.Fatalf("create expired session: %v", err)
	}
	handler := sm.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("protected handler should not run for expired session")
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/config", nil)
	req.AddCookie(&http.Cookie{Name: sm.CookieName(), Value: sess.SessionID})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"error":"unauthorized"}` {
		t.Fatalf("body = %q", got)
	}
	if _, err := sm.GetSession(sess.SessionID); err == nil {
		t.Fatal("expired session should be deleted during route validation")
	}
}
