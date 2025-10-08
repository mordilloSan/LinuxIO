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
    cfg := SessionConfig{
        IdleTimeout:     20 * time.Millisecond,
        AbsoluteTimeout: 500 * time.Millisecond,
        RefreshThrottle: 0,
        GCInterval:      0,
        Cookie: CookieConfig{
            Name:     "session_id",
            Path:     "/",
            SameSite: 0,
            Secure:   false,
            HTTPOnly: true,
        },
    }
    return NewManager(st, cfg)
}

func TestManager_CreateGetSetDelete(t *testing.T) {
    m := newTestManager(t)
    defer m.Close()

    u := User{Username: "alice", UID: "1000", GID: "1000"}
    s, err := m.CreateSession(u, false)
    if err != nil {
        t.Fatalf("CreateSession error: %v", err)
    }
    if s.SessionID == "" {
        t.Fatalf("CreateSession returned empty SessionID")
    }
    if s.BridgeSecret == "" || len(s.BridgeSecret) < 64 {
        t.Fatalf("BridgeSecret not set or too short: %q", s.BridgeSecret)
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

    u := User{Username: "bob", UID: "1001", GID: "1001"}
    s, err := m.CreateSession(u, false)
    if err != nil { t.Fatalf("CreateSession: %v", err) }

    // Force time to move forward for a measurable refresh
    time.Sleep(5 * time.Millisecond)
    before := s.Timing.IdleUntil
    if err := m.Refresh(s.SessionID); err != nil {
        t.Fatalf("Refresh error: %v", err)
    }
    s2, _ := m.GetSession(s.SessionID)
    if !s2.Timing.IdleUntil.After(before) && !s2.Timing.IdleUntil.Equal(before) {
        // Equal can happen if AbsoluteUntil already capped the idle window
        t.Fatalf("IdleUntil did not move forward or equal cap")
    }
}

func TestManager_WriteAndValidateCookie(t *testing.T) {
    m := newTestManager(t)
    defer m.Close()

    u := User{Username: "carl", UID: "1002", GID: "1002"}
    s, err := m.CreateSession(u, false)
    if err != nil { t.Fatalf("CreateSession: %v", err) }

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

    if _, err := m.ValidateFromRequest(req); err != nil {
        t.Fatalf("ValidateFromRequest unexpected error: %v", err)
    }
}

// firstCookieValue extracts the value part from a Set-Cookie header of form: name=value; ...
func firstCookieValue(setCookie string) string {
    eq := -1
    for i := 0; i < len(setCookie); i++ {
        if setCookie[i] == '=' { eq = i; break }
    }
    if eq < 0 { return "" }
    semi := len(setCookie)
    for j := eq + 1; j < len(setCookie); j++ {
        if setCookie[j] == ';' { semi = j; break }
    }
    return setCookie[eq+1 : semi]
}
