package session

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiagnosticRefDoesNotExposeSessionCredential(t *testing.T) {
	const credential = "0123456789abcdef0123456789abcdef"

	ref := DiagnosticRef(credential)
	if ref == "" {
		t.Fatal("DiagnosticRef returned an empty reference")
	}
	if len(ref) != len("sr-")+32 || !strings.HasPrefix(ref, "sr-") {
		t.Fatalf("DiagnosticRef = %q, want sr- plus 16 digest bytes", ref)
	}
	if strings.Contains(ref, credential) {
		t.Fatalf("diagnostic reference contains session credential: %q", ref)
	}
	if got := DiagnosticRef(credential); got != ref {
		t.Fatalf("DiagnosticRef is not stable: %q != %q", got, ref)
	}
	if got := DiagnosticRef("fedcba9876543210fedcba9876543210"); got == ref {
		t.Fatalf("different credentials produced the same reference: %q", got)
	}
	if got := DiagnosticRef(""); got != "" {
		t.Fatalf("DiagnosticRef(empty) = %q, want empty", got)
	}
}

func TestSessionLogsDoNotExposeUsername(t *testing.T) {
	const username = "sensitive-user"

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	cfg := DefaultConfig
	cfg.GCInterval = 0
	cfg.SingleSessionPerUser = true
	m := NewManager(cfg)
	defer m.Close()

	if _, err := m.CreateSession("deleted", User{Username: username}, false); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	m.DeleteSession("deleted", ReasonManual)

	for _, id := range []string{"first", "second"} {
		if _, err := m.CreateSession(id, User{Username: username}, false); err != nil {
			t.Fatalf("CreateSession(%q): %v", id, err)
		}
	}
	if strings.Contains(logs.String(), username) {
		t.Fatalf("session logs expose username: %q", logs.String())
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	cfg := DefaultConfig
	cfg.IdleTimeout = 20 * time.Millisecond
	cfg.AbsoluteTimeout = 500 * time.Millisecond
	cfg.RefreshThrottle = 0
	cfg.GCInterval = 0
	cfg.Cookie.SameSite = 0
	cfg.Cookie.Secure = false
	return NewManager(cfg)
}

func createTestSession(t *testing.T, m *Manager, user User, privileged bool) *Session {
	t.Helper()
	id, err := m.NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID: %v", err)
	}
	sess, err := m.CreateSession(id, user, privileged)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return sess
}

func TestManager_CreateGetDelete(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	u := User{Username: "alice", UID: 1000, GID: 1000}
	s := createTestSession(t, m, u, true)
	if s.SessionID == "" {
		t.Fatal("session has empty SessionID")
	}
	if s.Timing.IdleUntil.After(s.Timing.AbsoluteUntil) {
		t.Fatalf("IdleUntil should not be after AbsoluteUntil")
	}

	// Get
	got, err := m.GetSession(s.SessionID)
	if err != nil || got.SessionID != s.SessionID {
		t.Fatalf("GetSession mismatch got=%v err=%v", got, err)
	}
	if !got.Privileged {
		t.Fatal("privileged flag did not persist")
	}

	// Delete
	m.DeleteSession(s.SessionID, ReasonManual)
	if _, err := m.GetSession(s.SessionID); err == nil {
		t.Fatalf("GetSession should fail after delete")
	}
}

func TestManager_RefreshUpdatesIdle(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	u := User{Username: "bob", UID: 1001, GID: 1001}
	s := createTestSession(t, m, u, false)

	// Force time to move forward for a measurable refresh
	time.Sleep(5 * time.Millisecond)
	before := s.Timing.IdleUntil
	if err := m.Refresh(s.SessionID); err != nil {
		t.Fatalf("Refresh error: %v", err)
	}
	s2, _ := m.GetSession(s.SessionID)
	if !s2.Timing.IdleUntil.After(before) {
		t.Fatalf("IdleUntil did not move forward")
	}
}

func TestManager_NewSessionIDAndCreateSession(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	id, err := m.NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID: %v", err)
	}
	if id == "" {
		t.Fatal("NewSessionID returned empty id")
	}

	s, err := m.CreateSession(id, User{Username: "erin", UID: 1004, GID: 1004}, true)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s.SessionID != id {
		t.Fatalf("session id = %q, want %q", s.SessionID, id)
	}
	if !s.Privileged {
		t.Fatal("expected privileged session")
	}
}

func TestManager_WriteAndValidateCookieDoesNotRefresh(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	u := User{Username: "carl", UID: 1002, GID: 1002}
	s := createTestSession(t, m, u, false)

	// Write cookie
	rr := httptest.NewRecorder()
	m.WriteCookie(rr, s.SessionID)
	if rr.Header().Get("Set-Cookie") == "" {
		t.Fatalf("expected Set-Cookie header to be set")
	}

	// Validate from request
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	cookieVal := firstCookieValue(rr.Header().Get("Set-Cookie"))
	req.AddCookie(&http.Cookie{Name: m.CookieName(), Value: cookieVal})

	beforeIdle := s.Timing.IdleUntil
	beforeAccess := s.Timing.LastAccess
	time.Sleep(5 * time.Millisecond)
	got, err := m.ValidateFromRequest(req)
	if err != nil {
		t.Fatalf("ValidateFromRequest unexpected error: %v", err)
	}
	if !got.Timing.IdleUntil.Equal(beforeIdle) {
		t.Fatalf("ValidateFromRequest changed IdleUntil from %v to %v", beforeIdle, got.Timing.IdleUntil)
	}
	if !got.Timing.LastAccess.Equal(beforeAccess) {
		t.Fatalf("ValidateFromRequest changed LastAccess from %v to %v", beforeAccess, got.Timing.LastAccess)
	}
}

func TestManager_UsesExplicitCookieConfig(t *testing.T) {
	cfg := DefaultConfig
	cfg.GCInterval = 0
	cfg.Cookie.Domain = "example.com"
	cfg.Cookie.Path = "/custom"
	cfg.Cookie.SameSite = http.SameSiteLaxMode
	cfg.Cookie.Secure = false
	cfg.Cookie.HTTPOnly = false
	m := NewManager(cfg)
	defer m.Close()

	rr := httptest.NewRecorder()
	m.WriteCookie(rr, "abc")

	cookies := rr.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %d", len(cookies))
	}

	ck := cookies[0]
	if ck.Name != DefaultConfig.Cookie.Name {
		t.Fatalf("cookie name = %q, want %q", ck.Name, DefaultConfig.Cookie.Name)
	}
	if ck.Domain != "example.com" {
		t.Fatalf("cookie domain = %q, want example.com", ck.Domain)
	}
	if ck.Path != "/custom" {
		t.Fatalf("cookie path = %q, want /custom", ck.Path)
	}
	if ck.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie sameSite = %v, want %v", ck.SameSite, http.SameSiteLaxMode)
	}
	if ck.Secure {
		t.Fatal("cookie secure = true, want false")
	}
	if ck.HttpOnly {
		t.Fatal("cookie HttpOnly = true, want false")
	}
}

func TestManager_GCLoopDeletesAbsoluteExpiredSessions(t *testing.T) {
	cfg := DefaultConfig
	cfg.IdleTimeout = 100 * time.Millisecond
	cfg.AbsoluteTimeout = 20 * time.Millisecond
	cfg.RefreshThrottle = 0
	cfg.GCInterval = 5 * time.Millisecond
	cfg.Cookie.SameSite = 0
	cfg.Cookie.Secure = false
	m := NewManager(cfg)
	defer m.Close()

	done := make(chan DeleteReason, 1)
	m.RegisterOnDelete(func(_ *Session, reason DeleteReason) {
		select {
		case done <- reason:
		default:
		}
	})

	s := createTestSession(t, m, User{Username: "dana", UID: 1003, GID: 1003}, false)

	select {
	case reason := <-done:
		if reason != ReasonGCAbsolute {
			t.Fatalf("delete reason = %q, want %q", reason, ReasonGCAbsolute)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("timed out waiting for absolute-expiry GC")
	}

	if _, err := m.GetSession(s.SessionID); err == nil {
		t.Fatalf("expected session deleted after absolute-expiry GC")
	}
}

func TestManager_RefreshDoesNotResurrectDeletedSession(t *testing.T) {
	cfg := DefaultConfig
	cfg.RefreshThrottle = 0
	cfg.GCInterval = 0
	m := NewManager(cfg)
	defer m.Close()

	var deletes atomic.Int32
	m.RegisterOnDelete(func(*Session, DeleteReason) { deletes.Add(1) })

	for i := range 200 {
		s := createTestSession(t, m, User{Username: "race"}, false)
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() { _ = m.Refresh(s.SessionID) })
			wg.Go(func() { m.DeleteSession(s.SessionID, ReasonLogout) })
		}
		wg.Wait()
		if _, err := m.ValidateSession(s.SessionID); err == nil {
			t.Fatalf("iteration %d: deleted session became valid again", i)
		}
	}

	deadline := time.Now().Add(time.Second)
	for deletes.Load() < 200 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(10 * time.Millisecond)
	if got := deletes.Load(); got != 200 {
		t.Fatalf("delete hooks fired %d times, want once per session (200)", got)
	}
}

func TestDefaultCookieUsesHostPrefix(t *testing.T) {
	c := DefaultConfig.Cookie
	if !strings.HasPrefix(c.Name, "__Host-") || !c.Secure || c.Path != "/" || c.Domain != "" {
		t.Fatalf("default cookie %+v does not satisfy __Host- prefix rules", c)
	}
}

// firstCookieValue extracts the value part from a Set-Cookie header of form: name=value; ...
func firstCookieValue(setCookie string) string {
	eq := -1
	for i := 0; i < len(setCookie); i++ {
		if setCookie[i] == '=' {
			eq = i
			break
		}
	}
	if eq < 0 {
		return ""
	}
	semi := len(setCookie)
	for j := eq + 1; j < len(setCookie); j++ {
		if setCookie[j] == ';' {
			semi = j
			break
		}
	}
	return setCookie[eq+1 : semi]
}
