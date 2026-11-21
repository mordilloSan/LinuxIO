package appupdate

import (
	"net/http"
)

// RegisterRoutes wires the version endpoint. It is public: the page reads it
// to see that a restarted webserver answers again.
func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/version", handleVersion)
}
