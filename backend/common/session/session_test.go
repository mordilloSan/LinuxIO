package session

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	st := NewWithCleanupInterval(0)
	cfg := DefaultConfig
	cfg.IdleTimeout = 20 * time.Millisecond
	cfg.AbsoluteTimeout = 500 * time.Millisecond
	cfg.RefreshThrottle = 0
	cfg.GCInterval = 0
	cfg.Cookie.SameSite = 0
	cfg.Cookie.Secure = false
	return NewManager(st, cfg)
}

func TestManager_CreateGetSetDelete(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	u := User{Username: "alice", UID: 1000, GID: 1000}
	s, err := m.CreateSession(u, false)
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}
	if s.SessionID == "" {
		t.Fatalf("CreateSession returned empty SessionID")
	}
	if s.Timing.IdleUntil.After(s.Timing.AbsoluteUntil) {
		t.Fatalf("IdleUntil should not be after AbsoluteUntil")
	}

	// Get
	got, err := m.GetSession(s.SessionID)
	if err != nil || got.SessionID != s.SessionID {
		t.Fatalf("GetSession mismatch got=%v err=%v", got, err)
	}

	// Set privileged
	if err := m.SetPrivileged(s.SessionID, true); err != nil {
		t.Fatalf("SetPrivileged error: %v", err)
	}
	got2, _ := m.GetSession(s.SessionID)
	if !got2.Privileged {
		t.Fatalf("SetPrivileged did not persist")
	}

	caps := Capabilities{
		DockerAvailable:        true,
		IndexerAvailable:       true,
		LMSensorsAvailable:     false,
		SmartmontoolsAvailable: true,
	}
	if err := m.SetCapabilities(s.SessionID, caps); err != nil {
		t.Fatalf("SetCapabilities error: %v", err)
	}
	got3, _ := m.GetSession(s.SessionID)
	if got3.Capabilities != caps {
		t.Fatalf("SetCapabilities did not persist: got=%+v want=%+v", got3.Capabilities, caps)
	}

	// Delete
	if err := m.DeleteSession(s.SessionID, ReasonManual); err != nil {
		t.Fatalf("DeleteSession error: %v", err)
	}
	if _, err := m.GetSession(s.SessionID); err == nil {
		t.Fatalf("GetSession should fail after delete")
	}
}

func TestManager_RefreshUpdatesIdle(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	u := User{Username: "bob", UID: 1001, GID: 1001}
	s, err := m.CreateSession(u, false)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

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

func TestManager_NewSessionIDAndCreateSessionWithID(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	id, err := m.NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID: %v", err)
	}
	if id == "" {
		t.Fatal("NewSessionID returned empty id")
	}

	s, err := m.CreateSessionWithID(id, User{Username: "erin", UID: 1004, GID: 1004}, true)
	if err != nil {
		t.Fatalf("CreateSessionWithID: %v", err)
	}
	if s.SessionID != id {
		t.Fatalf("session id = %q, want %q", s.SessionID, id)
	}
	if !s.Privileged {
		t.Fatal("expected privileged session")
	}
}

func TestManager_WriteAndValidateCookie(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	u := User{Username: "carl", UID: 1002, GID: 1002}
	s, err := m.CreateSession(u, false)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Write cookie
	rr := httptest.NewRecorder()
	m.WriteCookie(rr, s.SessionID)
	if rr.Header().Get("Set-Cookie") == "" {
		t.Fatalf("expected Set-Cookie header to be set")
	}

	// Validate from request
	req := httptest.NewRequest("GET", "/", nil)
	cookieVal := firstCookieValue(rr.Header().Get("Set-Cookie"))
	req.AddCookie(&http.Cookie{Name: m.CookieName(), Value: cookieVal})

	before := s.Timing.IdleUntil
	time.Sleep(5 * time.Millisecond)
	got, err := m.ValidateFromRequest(req)
	if err != nil {
		t.Fatalf("ValidateFromRequest unexpected error: %v", err)
	}
	if !got.Timing.IdleUntil.After(before) {
		t.Fatalf("ValidateFromRequest should return refreshed timing")
	}
}

func TestManager_UsesExplicitCookieConfig(t *testing.T) {
	st := NewWithCleanupInterval(0)
	cfg := DefaultConfig
	cfg.GCInterval = 0
	cfg.Cookie.Domain = "example.com"
	cfg.Cookie.Path = "/custom"
	cfg.Cookie.SameSite = http.SameSiteLaxMode
	cfg.Cookie.Secure = false
	cfg.Cookie.HTTPOnly = false
	m := NewManager(st, cfg)
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
	st := NewWithCleanupInterval(0)
	cfg := DefaultConfig
	cfg.IdleTimeout = 100 * time.Millisecond
	cfg.AbsoluteTimeout = 20 * time.Millisecond
	cfg.RefreshThrottle = 0
	cfg.GCInterval = 5 * time.Millisecond
	cfg.Cookie.SameSite = 0
	cfg.Cookie.Secure = false
	m := NewManager(st, cfg)
	defer m.Close()

	done := make(chan DeleteReason, 1)
	m.RegisterOnDelete(func(_ *Session, reason DeleteReason) {
		select {
		case done <- reason:
		default:
		}
	})

	s, err := m.CreateSession(User{Username: "dana", UID: 1003, GID: 1003}, false)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

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
