package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
	"github.com/mordilloSan/LinuxIO/backend/common/socketactivation"
	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

type connectionKindContextKey struct{}

func unixConnContext(ctx context.Context, c net.Conn) context.Context {
	ctx = context.WithValue(ctx, connectionKindContextKey{}, true)
	return peercred.ConnContext(ctx, c)
}

func requestFromUnixSocket(r *http.Request) bool {
	unix, _ := r.Context().Value(connectionKindContextKey{}).(bool)
	return unix
}

func loadActivatedListener() (net.Listener, error) {
	listeners, err := socketactivation.Listeners()
	if err != nil {
		return nil, fmt.Errorf("load socket activation listeners: %w", err)
	}
	if len(listeners) != 1 || listeners[0].Addr().Network() != "unix" {
		socketactivation.CloseListeners(listeners)
		return nil, fmt.Errorf("systemd socket activation requires exactly one Unix listener")
	}
	return listeners[0], nil
}

// Run starts the HTTP server and blocks until context is cancelled.
func (d *daemon) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if d.bgCancel != nil {
		d.bgCancel()
	}
	d.bgCancel = cancel

	d.markActivity()
	d.goBackground(func() { d.stopWhenIdle(runCtx, cancel, runtimeDirectoryExists, idleCheckInterval) })

	listener, err := loadActivatedListener()
	if err != nil {
		return err
	}
	return d.serveHTTP(runCtx, listener)
}

func (d *daemon) serveHTTP(ctx context.Context, listener net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc(api.RouteIndex, func(w http.ResponseWriter, r *http.Request) {
		d.handleIndex(ctx, w, r)
	})
	mux.HandleFunc(api.RouteReindex, func(w http.ResponseWriter, r *http.Request) {
		d.handleReindex(ctx, w, r)
	})
	mux.HandleFunc(api.RouteStatus, d.handleStatus)
	mux.HandleFunc(api.RouteSearch, d.handleSearch)
	mux.HandleFunc(api.RouteDirSize, d.handleDirSize)
	mux.HandleFunc(api.RouteSubfolders, d.handleSubfolders)
	mux.HandleFunc(api.RouteAdd, d.handleAdd)
	mux.HandleFunc(api.RouteDelete, d.handleDelete)
	mux.HandleFunc(api.RouteConfig, d.handleConfig)

	handler := d.activityMiddleware(loggerMiddleware(recoveryMiddleware(authorizeTransportMiddleware(mux))))

	errCh := make(chan error, 1)
	srv := newHTTPServer(handler)
	srv.ConnContext = unixConnContext
	srv.BaseContext = func(net.Listener) context.Context { return ctx }
	d.servers = append(d.servers, srv)
	slog.Info("API listening", "addr", "unix://"+listener.Addr().String(), "systemd_socket_activation", true)
	go func() { errCh <- srv.Serve(listener) }()

	select {
	case <-ctx.Done():
		d.shutdownHTTPServers()
		return nil
	case err := <-errCh:
		d.shutdownHTTPServers()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (d *daemon) shutdownHTTPServers() {
	ctx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
	defer cancel()
	for _, srv := range d.servers {
		err := srv.Shutdown(ctx)
		if errors.Is(err, context.DeadlineExceeded) {
			slog.Warn("graceful server shutdown timed out; forcing close")
			err = srv.Close()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("server shutdown error", "err", err)
		}
	}
}

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       httpReadTimeout,
		// SSE responses can legitimately remain open for an entire index run.
		WriteTimeout:   0,
		IdleTimeout:    httpIdleTimeout,
		MaxHeaderBytes: httpMaxHeaderBytes,
	}
}
