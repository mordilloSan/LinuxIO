// Package versionapi serves /api/version, the public endpoint the page polls
// to see that a restarted webserver answers again.
package versionapi

import (
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
	"github.com/mordilloSan/LinuxIO/backend/webserver/web"
)

// RegisterRoutes wires the version endpoint. It needs no sign-in.
func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/version", handleVersion)
}

// versionResponse is the body of /api/version: the build of this webserver,
// which is the version of the installed package.
type versionResponse struct {
	Version string `json:"version"`
}

func handleVersion(w http.ResponseWriter, _ *http.Request) {
	web.WriteJSON(w, http.StatusOK, versionResponse{Version: version.Version})
}
