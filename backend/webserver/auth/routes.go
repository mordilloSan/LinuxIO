package auth

import (
	"fmt"
	"net/http"
	"os/user"
	"strconv"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/webserver/bridge"
)

// createSessionUser wraps os/user.Lookup and converts to session.User at the system boundary
func createSessionUser(username string) (session.User, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return session.User{}, err
	}

	uid64, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return session.User{}, fmt.Errorf("invalid UID for user %s: %w", username, err)
	}
	gid64, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return session.User{}, fmt.Errorf("invalid GID for user %s: %w", username, err)
	}

	return session.User{
		Username: u.Username,
		UID:      uint32(uid64),
		GID:      uint32(gid64),
	}, nil
}

// --- test seams (overridden in tests) ---
var (
	startBridge = bridge.StartBridge
	lookupUser  = createSessionUser
)

// RegisterAuthRoutes wires public and private auth endpoints under /auth.
func RegisterAuthRoutes(mux *http.ServeMux, sm *session.Manager, verbose bool) {
	h := &Handlers{
		SM:      sm,
		Verbose: verbose,
	}

	// public
	mux.HandleFunc("POST /auth/login", h.Login)
	mux.HandleFunc("GET /api/version", h.Version)
	mux.HandleFunc("GET /api/update-status", h.UpdateStatus)

	// private (wrapped with session middleware)
	mux.Handle("GET /auth/logout", sm.RequireSession(http.HandlerFunc(h.Logout)))
}
