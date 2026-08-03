//go:build pprofdebug

// Package debugserver serves net/http/pprof on a loopback listener in debug
// builds. Production builds (without the pprofdebug tag) compile the no-op
// variant, so no profiling endpoint ever ships.
//
// Built with GOEXPERIMENT=goroutineleakprofile (see `make build-leak-profile`),
// the profile index additionally exposes /debug/pprof/goroutineleak, which
// reports goroutines the runtime has proven can never unblock.
package debugserver

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"time"
)

// Start serves the pprof handlers on addr in a background goroutine.
// addr must be a loopback address; the listener carries no authentication.
// Failure to bind (e.g. a second bridge on the same port) only logs a warning.
func Start(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	go func() {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			slog.Warn("pprof debug server failed to bind", "addr", addr, "error", err)
			return
		}
		slog.Info("pprof debug server listening", "url", "http://"+ln.Addr().String()+"/debug/pprof/")
		srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		if serveErr := srv.Serve(ln); serveErr != nil {
			slog.Warn("pprof debug server stopped", "error", serveErr)
		}
	}()
}
