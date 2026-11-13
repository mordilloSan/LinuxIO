package session

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type User struct {
	Username string `json:"username"`
	UID      uint32 `json:"uid"`
	GID      uint32 `json:"gid"`
}

type Timing struct {
	CreatedAt     time.Time `json:"created_at"`
	LastAccess    time.Time `json:"last_access"`
	LastRefresh   time.Time `json:"last_refresh"`
	IdleUntil     time.Time `json:"idle_until"`
	AbsoluteUntil time.Time `json:"absolute_until"`
}

// Session is the cross-process session record shared by the webserver, the
// auth daemon and the bridge. The webserver-only manager lives in
// backend/webserver/sessions.
type Session struct {
	SessionID  string `json:"session_id"`
	User       User   `json:"user"`
	Privileged bool   `json:"privileged"`
	Timing     Timing `json:"timing"`
}

// DiagnosticRef returns a stable, non-secret correlation value for a session
// credential. Production session IDs contain 128 bits of randomness, so a
// one-way digest preserves diagnostic correlation without disclosing the
// bearer credential itself. The result is diagnostic-only and must never be
// accepted for authentication or authorization.
func DiagnosticRef(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("linuxio-session-diagnostic-ref\x00" + sessionID))
	return "sr-" + hex.EncodeToString(sum[:16])
}
