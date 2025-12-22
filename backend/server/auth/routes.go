package auth

import (
	"net/http"
	"os/user"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
)

// --- test seams (overridden in tests) ---
var (
	startBridge        = bridge.StartBridge
	callBridgeWithSess = bridge.CallTypedWithSession
	getBridgeBinary    = bridge.GetBridgeBinaryPath
	lookupUser         = user.Lookup
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
