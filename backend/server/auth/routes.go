package auth

import (
	"fmt"
	"net/http"
	"os/user"
	"strconv"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
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
	startBridge        = bridge.StartBridge
	callBridgeWithSess = bridge.CallTypedWithSession
	getBridgeBinary    = bridge.GetBridgeBinaryPath
	lookupUser         = createSessionUser
)

// RegisterAuthRoutes wires public and private auth endpoints under /auth.
func RegisterAuthRoutes(mux *http.ServeMux, sm *session.Manager, env string, verbose bool, bridgeBinaryOverride string) {
	h := &Handlers{
		SM:                   sm,
		Env:                  env,
		Verbose:              verbose,
		BridgeBinaryOverride: bridgeBinaryOverride,
	}

	// public
	mux.HandleFunc("POST /auth/login", h.Login)

	// private (wrapped with session middleware)
	mux.Handle("GET /auth/me", sm.RequireSession(http.HandlerFunc(h.Me)))
	mux.Handle("GET /auth/logout", sm.RequireSession(http.HandlerFunc(h.Logout)))
}
