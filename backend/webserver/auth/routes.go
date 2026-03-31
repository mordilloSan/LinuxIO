package auth

import (
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/webserver/bridge"
)

// --- test seams (overridden in tests) ---
var (
	startBridge = bridge.StartBridge
)

// RegisterAuthRoutes wires public and private auth endpoints under /auth.
func RegisterAuthRoutes(mux *http.ServeMux, sm *session.Manager, verbose bool) {
	h := &Handlers{
		SM:      sm,
		Verbose: verbose,
		authSem: make(chan struct{}, maxConcurrentLogins),
	}

	// public
	mux.HandleFunc("POST /auth/login", h.Login)
	mux.HandleFunc("GET /api/version", h.Version)
	mux.HandleFunc("GET /api/update-status", h.UpdateStatus)

	// private (wrapped with session middleware)
	mux.Handle("GET /auth/logout", sm.RequireSession(http.HandlerFunc(h.Logout)))
}
