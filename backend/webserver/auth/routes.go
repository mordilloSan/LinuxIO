package auth

import (
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/webserver/bridge"
	"github.com/mordilloSan/LinuxIO/backend/webserver/sessions"
)

// --- test seams (overridden in tests) ---
var startBridge = bridge.StartBridge

// RegisterAuthRoutes wires the login and logout endpoints.
func RegisterAuthRoutes(mux *http.ServeMux, sm *sessions.Manager) {
	h := &Handlers{
		SM:      sm,
		authSem: make(chan struct{}, maxConcurrentLogins),
	}

	// public
	mux.HandleFunc("POST /auth/login", h.Login)

	// private (wrapped with session middleware)
	mux.Handle("GET /auth/logout", sm.RequireSession(http.HandlerFunc(h.Logout)))
}
