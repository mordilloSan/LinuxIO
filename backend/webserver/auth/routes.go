package auth

import (
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/webserver/bridge"
)

// --- test seams (overridden in tests) ---
var (
	startBridge    = bridge.StartBridge
	checkForUpdate = CheckForUpdate
)

// RegisterAuthRoutes wires authentication, version, and update endpoints.
func RegisterAuthRoutes(mux *http.ServeMux, sm *session.Manager, verbose bool) {
	h := &Handlers{
		SM:      sm,
		Verbose: verbose,
		authSem: make(chan struct{}, maxConcurrentLogins),
	}

	// public
	mux.HandleFunc("POST /auth/login", h.Login)
	mux.HandleFunc("GET /api/version", h.Version)

	// private (wrapped with session middleware)
	mux.Handle("GET /auth/logout", sm.RequireSession(http.HandlerFunc(h.Logout)))
	mux.Handle("GET /api/update-info", sm.RequireSession(http.HandlerFunc(h.UpdateInfo)))
	mux.Handle("GET /api/update-status", sm.RequireSession(http.HandlerFunc(h.UpdateStatus)))
}
