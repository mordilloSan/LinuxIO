package appupdate

import (
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/webserver/sessions"
)

// RegisterRoutes wires the version, update-info and update-status endpoints.
func RegisterRoutes(mux *http.ServeMux, sm *sessions.Manager) {
	// public
	mux.HandleFunc("GET /api/version", handleVersion)

	// private (wrapped with session middleware)
	mux.Handle("GET /api/update-info", sm.RequireSession(http.HandlerFunc(handleUpdateInfo)))
	mux.Handle("GET /api/update-status", sm.RequireSession(http.HandlerFunc(handleUpdateStatus)))
}
