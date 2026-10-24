package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/webserver/sessions"
)

func newTestSessionManager(cfg sessions.SessionConfig) *sessions.Manager {
	cfg.Cookie.Secure = false
	cfg.GCInterval = 0
	return sessions.NewManager(cfg)
}

// watchDeletes records delete-hook reasons. Expired-session tests assert on
// the hook rather than re-reading the table, because a validating read would
// itself delete the expired session and mask a missed deletion.
func watchDeletes(sm *sessions.Manager) <-chan sessions.DeleteReason {
	ch := make(chan sessions.DeleteReason, 4)
	sm.RegisterOnDelete(func(_ *session.Session, r sessions.DeleteReason) {
		select {
		case ch <- r:
		default:
		}
	})
	return ch
}

func requireDeleted(t *testing.T, deletes <-chan sessions.DeleteReason, want sessions.DeleteReason) {
	t.Helper()
	select {
	case got := <-deletes:
		if got != want {
			t.Fatalf("delete reason = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("session was not deleted")
	}
}

func TestWebSocketAuthMiddlewareRejectsMissingSessionWithPolicyViolation(t *testing.T) {
	sm := newTestSessionManager(sessions.DefaultConfig)
	defer sm.Close()

	nextCalled := false
	server := httptest.NewServer(AuthMiddleware(sm, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	cfg := sessions.DefaultConfig
	cfg.IdleTimeout = -time.Second
	sm := newTestSessionManager(cfg)
	defer sm.Close()
	deletes := watchDeletes(sm)

	sess, err := sm.CreateSession("expired-session", session.User{Username: "miguel", UID: 1000, GID: 1000}, false)
	if err != nil {
		t.Fatalf("create expired session: %v", err)
	}

	server := httptest.NewServer(AuthMiddleware(sm, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	requireDeleted(t, deletes, sessions.ReasonGCIdle)
}

func TestCloseWebSocketForSessionSendsSessionExpiredPolicyViolation(t *testing.T) {
	ready := make(chan struct{})
	done := make(chan struct{})
	handlerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
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

	CloseForSession("session-1")
	close(done)
	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("websocket handler did not exit")
	}

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
	wsConnsBySession.Lock()
	_, registered := wsConnsBySession.conns["session-1"]
	wsConnsBySession.Unlock()
	if registered {
		t.Fatal("session websocket registry entry should be removed")
	}
}

func TestProtectedRouteReturnsUnauthorizedForExpiredSessionCookie(t *testing.T) {
	cfg := sessions.DefaultConfig
	cfg.IdleTimeout = -time.Second
	sm := newTestSessionManager(cfg)
	defer sm.Close()
	deletes := watchDeletes(sm)

	sess, err := sm.CreateSession("expired-session", session.User{Username: "miguel", UID: 1000, GID: 1000}, false)
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
	requireDeleted(t, deletes, sessions.ReasonGCIdle)
}

func TestWebSocketActivityRefreshIsExplicitAndExpiryStopsFrames(t *testing.T) {
	cfg := sessions.DefaultConfig
	cfg.IdleTimeout = 500 * time.Millisecond
	cfg.RefreshThrottle = 0
	sm := newTestSessionManager(cfg)
	defer sm.Close()
	deletes := watchDeletes(sm)

	sess, err := sm.CreateSession("activity-session", session.User{Username: "miguel", UID: 1000, GID: 1000}, false)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	server := httptest.NewServer(AuthMiddleware(sm, RelayHandler(sm)))
	defer server.Close()
	header := http.Header{}
	header.Add("Cookie", (&http.Cookie{Name: sm.CookieName(), Value: sess.SessionID}).String())
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL, "/ws"), header)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	beforeHandshake, err := sm.ValidateSession(sess.SessionID)
	if err != nil {
		t.Fatalf("get session after handshake: %v", err)
	}
	if writeErr := conn.WriteMessage(websocket.BinaryMessage, []byte{0, 0, 0, 0, FlagDATA}); writeErr != nil {
		t.Fatalf("write passive frame: %v", writeErr)
	}
	time.Sleep(20 * time.Millisecond)
	afterPassive, err := sm.ValidateSession(sess.SessionID)
	if err != nil {
		t.Fatalf("get session after passive frame: %v", err)
	}
	if !afterPassive.Timing.IdleUntil.Equal(beforeHandshake.Timing.IdleUntil) {
		t.Fatalf("passive frame refreshed IdleUntil from %v to %v", beforeHandshake.Timing.IdleUntil, afterPassive.Timing.IdleUntil)
	}
	if writeErr := conn.WriteMessage(websocket.BinaryMessage, []byte{0, 0, 0, 0, FlagActivity}); writeErr != nil {
		t.Fatalf("write activity frame: %v", writeErr)
	}
	deadline := time.Now().Add(time.Second)
	var afterActivity *session.Session
	for time.Now().Before(deadline) {
		afterActivity, err = sm.ValidateSession(sess.SessionID)
		if err == nil && afterActivity.Timing.IdleUntil.After(afterPassive.Timing.IdleUntil) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if afterActivity == nil || !afterActivity.Timing.IdleUntil.After(afterPassive.Timing.IdleUntil) {
		t.Fatalf("activity frame did not refresh IdleUntil")
	}

	cfg.IdleTimeout = 10 * time.Millisecond
	// The existing session keeps its original deadline; expire it by waiting
	// past the deadline, then ensure an inbound frame is rejected.
	time.Sleep(time.Until(afterActivity.Timing.IdleUntil) + 20*time.Millisecond)
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0, 0, 0, 0, FlagDATA}); err != nil {
		t.Fatalf("write expired-session frame: %v", err)
	}
	_, _, readErr := conn.ReadMessage()
	if readErr == nil {
		t.Fatal("expected expired-session websocket to close")
	}
	requireDeleted(t, deletes, sessions.ReasonGCIdle)
}

func TestValidateSessionActivityDoesNotRefresh(t *testing.T) {
	m := newTestSessionManager(sessions.DefaultConfig)
	defer m.Close()
	sess, err := m.CreateSession("pong-session", session.User{Username: "miguel", UID: 1000, GID: 1000}, false)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	before, err := m.ValidateSession(sess.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if validateErr := validateSessionActivity(m, sess.SessionID); validateErr != nil {
		t.Fatalf("validate activity: %v", validateErr)
	}
	after, err := m.ValidateSession(sess.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Timing.IdleUntil.Equal(before.Timing.IdleUntil) || !after.Timing.LastAccess.Equal(before.Timing.LastAccess) {
		t.Fatal("read-only session validation changed activity timing")
	}
}

func wsURL(httpURL string, path string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + path
}
