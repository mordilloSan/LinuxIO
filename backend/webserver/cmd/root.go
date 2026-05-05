package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/logging"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/webserver/auth"
	"github.com/mordilloSan/LinuxIO/backend/webserver/bridge"
	"github.com/mordilloSan/LinuxIO/backend/webserver/web"
)

func RunServer(cfg ServerConfig) {
	if err := logging.Configure("linuxio-webserver", cfg.Verbose); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	slog.Info("LinuxIO starting", "verbose", cfg.Verbose)

	sm := newSessionManager()
	srv, inFlight, lastHit := newHTTPServer(cfg, sm)
	quit, done := startHTTPServer(cfg, srv, sm, inFlight, lastHit)

	waitForServerShutdown(quit, done)
	shutdownHTTPServer(srv)
	closeBridgeSessions(sm)
	sm.Close()
	slog.Info("server stopped")
}

func newSessionManager() *session.Manager {
	ms := session.New()
	sessionCfg := session.DefaultConfig
	sessionCfg.SingleSessionPerUser = false
	sm := session.NewManager(ms, sessionCfg)
	sm.RegisterOnDelete(func(sess *session.Session, reason session.DeleteReason) {
		if sess.User.Username == "" {
			return
		}
		bridge.CloseYamuxSession(sess.SessionID)
		web.CloseWebSocketForSession(sess.SessionID)
	})
	return sm
}

func newHTTPServer(cfg ServerConfig, sm *session.Manager) (*http.Server, *atomic.Int64, *atomic.Int64) {
	ui, err := web.UI()
	if err != nil {
		slog.Error("failed to mount embedded frontend", "error", err)
		os.Exit(1)
	}

	router := web.BuildRouter(web.Config{
		Verbose: cfg.Verbose,
		UI:      ui,
		RegisterRoutes: func(mux *http.ServeMux) {
			auth.RegisterAuthRoutes(mux, sm, cfg.Verbose)
		},
	}, sm)

	var inFlight atomic.Int64
	var lastHit atomic.Int64
	lastHit.Store(time.Now().UnixNano())

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastHit.Store(time.Now().UnixNano())
		inFlight.Add(1)
		defer inFlight.Add(-1)
		router.ServeHTTP(w, r)
	})

	return &http.Server{
		Addr:     fmt.Sprintf(":%d", cfg.Port),
		Handler:  handler,
		ErrorLog: log.New(web.HTTPErrorLogAdapter{}, "", 0),
	}, &inFlight, &lastHit
}

func startHTTPServer(
	cfg ServerConfig,
	srv *http.Server,
	sm *session.Manager,
	inFlight *atomic.Int64,
	lastHit *atomic.Int64,
) (chan os.Signal, chan struct{}) {
	quit := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if serveWithSocketActivation(cfg, srv, sm, inFlight, lastHit, done) {
			return
		}
		serveWithSelfBind(cfg, srv, done)
	}()

	return quit, done
}

func serveWithSocketActivation(
	cfg ServerConfig,
	srv *http.Server,
	sm *session.Manager,
	inFlight *atomic.Int64,
	lastHit *atomic.Int64,
	done chan struct{},
) bool {
	listeners, err := systemdListeners()
	if err != nil {
		slog.Warn("socket activation listener lookup failed", "error", err)
	}
	if len(listeners) == 0 {
		return false
	}

	configureServerTLS(srv)

	var stopOnce sync.Once
	servStopped := make(chan struct{})
	stop := func() { stopOnce.Do(func() { close(servStopped) }) }

	for _, listener := range listeners {
		go serveTLSListener(srv, web.NewTLSRedirectListener(listener, srv.TLSConfig, cfg.Port), stop, "server error (TLS)")
	}
	slog.Info("socket-activated HTTPS server listening")
	startSocketIdleExitWatcher(
		srv, sm, inFlight, lastHit,
		90*time.Second, 15*time.Second,
	)

	<-servStopped
	close(done)
	return true
}

func serveWithSelfBind(cfg ServerConfig, srv *http.Server, done chan struct{}) {
	configureServerTLS(srv)

	listener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		slog.Error("listen failed", "address", srv.Addr, "error", err)
		os.Exit(1)
	}
	tlsListener := web.NewTLSRedirectListener(listener, srv.TLSConfig, cfg.Port)
	slog.Info("HTTPS server listening", "address", fmt.Sprintf("https://localhost:%d", cfg.Port))
	if err := srv.Serve(tlsListener); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
	close(done)
}

func configureServerTLS(srv *http.Server) {
	cert, err := web.GenerateSelfSignedCert()
	if err != nil {
		slog.Error("failed to generate certificate", "error", err)
		os.Exit(1)
	}
	srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
}

func serveTLSListener(srv *http.Server, listener net.Listener, stop func(), errorPrefix string) {
	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		slog.Error(errorPrefix, "error", err)
		os.Exit(1)
	}
	stop()
}

func waitForServerShutdown(quit <-chan os.Signal, done <-chan struct{}) {
	select {
	case <-quit:
		slog.Info("shutdown signal received")
	case <-done:
		slog.Info("HTTP server stopped, beginning shutdown")
	}
}

func shutdownHTTPServer(srv *http.Server) {
	srv.SetKeepAlivesEnabled(false)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		if errors.Is(err, context.DeadlineExceeded) {
			slog.Warn("graceful HTTP shutdown timed out; forcing close")
			if cerr := srv.Close(); cerr != nil && !errors.Is(cerr, http.ErrServerClosed) {
				slog.Warn("HTTP server force-close failed", "error", cerr)
			}
			return
		}
		slog.Warn("HTTP server shutdown failed", "error", err)
		return
	}
	slog.Info("HTTP server closed")
}

func closeBridgeSessions(sm *session.Manager) {
	sessions, err := sm.ActiveSessions()
	if err != nil {
		return
	}
	for _, sess := range sessions {
		bridge.CloseYamuxSession(sess.SessionID)
	}
}

func startSocketIdleExitWatcher(
	srv *http.Server,
	sm *session.Manager,
	inFlight *atomic.Int64,
	lastHit *atomic.Int64,
	idleGrace time.Duration,
	checkEvery time.Duration,
) {
	if idleGrace <= 0 || checkEvery <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(checkEvery)
		defer t.Stop()
		for range t.C {
			// no requests running?
			if inFlight.Load() > 0 {
				continue
			}
			// no recent hits?
			if time.Since(time.Unix(0, lastHit.Load())) < idleGrace {
				continue
			}
			// no active sessions?
			act, err := sm.ActiveSessions()
			if err != nil {
				continue
			}
			if len(act) > 0 {
				continue
			}

			slog.Info("socket idle exit triggered", "idle_grace", idleGrace)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := srv.Shutdown(ctx); err != nil {
				slog.Warn("idle shutdown failed", "idle_grace", idleGrace, "error", err)
			}
			cancel()
			return
		}
	}()
}
