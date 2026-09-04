package app

import (
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
)

// requireRootPeer serves the control socket: only a root peer may call it.
func requireRootPeer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, ok := peercred.RequestUID(r)
		if !ok || uid != 0 {
			http.Error(w, "this endpoint requires a root peer", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
