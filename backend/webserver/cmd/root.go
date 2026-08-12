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
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/debugserver"
	"github.com/mordilloSan/LinuxIO/backend/common/logging"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
	"github.com/mordilloSan/LinuxIO/backend/webserver/auth"
	"github.com/mordilloSan/LinuxIO/backend/webserver/bridge"
	"github.com/mordilloSan/LinuxIO/backend/webserver/web"
)

func RunServer(cfg ServerConfig) error {
	if err := logging.Configure("linuxio-webserver", cfg.Verbose); err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	slog.Info("LinuxIO starting", "verbose", cfg.Verbose)
	debugserver.Start("127.0.0.1:6060")

	sm := newSessionManager()
	srv, activity, err := newHTTPServer(cfg, sm)
	if err != nil {
		return err
	}
	quit, done := startHTTPServer(cfg, srv, sm, activity)

	serverErr := waitForServerShutdown(quit, done)
	if serverErr == nil {
		shutdownHTTPServer(srv)
	}
	closeBridgeSessions(sm)
	sm.Close()
	slog.Info("server stopped")
	return serverErr
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

type serverActivity struct {
	inFlight atomic.Int64
	lastHit  atomic.Int64
}

// HTTP connection limits protect the public listener from slow handshakes and
// abandoned keep-alive connections. WriteTimeout intentionally remains zero:
// websocket and streaming handlers may write for an unbounded duration.
const (
	httpReadHeaderTimeout = 10 * time.Second
	httpIdleTimeout       = 2 * time.Minute
)

func (a *serverActivity) idleFor(duration time.Duration) bool {
	return a.inFlight.Load() == 0 && time.Since(time.Unix(0, a.lastHit.Load())) >= duration
}

func newHTTPServer(cfg ServerConfig, sm *session.Manager) (*http.Server, *serverActivity, error) {
	ui, err := web.UI()
	if err != nil {
		slog.Error("failed to mount embedded frontend", "error", err)
		return nil, nil, fmt.Errorf("mount embedded frontend: %w", err)
	}

	router := web.BuildRouter(web.Config{
		Verbose: cfg.Verbose,
		UI:      ui,
		RegisterRoutes: func(mux *http.ServeMux) {
			auth.RegisterAuthRoutes(mux, sm, cfg.Verbose)
		},
	}, sm)

	activity := &serverActivity{}
	activity.lastHit.Store(time.Now().UnixNano())

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		activity.lastHit.Store(time.Now().UnixNano())
		activity.inFlight.Add(1)
		defer activity.inFlight.Add(-1)
		router.ServeHTTP(w, r)
	})

	return &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           handler,
		ErrorLog:          log.New(web.HTTPErrorLogAdapter{}, "", 0),
		ReadHeaderTimeout: httpReadHeaderTimeout,
		IdleTimeout:       httpIdleTimeout,
	}, activity, nil
}

func startHTTPServer(
	cfg ServerConfig,
	srv *http.Server,
	sm *session.Manager,
	activity *serverActivity,
) (chan os.Signal, chan error) {
	quit := make(chan os.Signal, 1)
	done := make(chan error, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		handled, err := serveWithSocketActivation(cfg, srv, sm, activity)
		if handled {
			done <- err
			return
		}
		done <- serveWithSelfBind(cfg, srv)
	}()

	return quit, done
}

func serveWithSocketActivation(
	cfg ServerConfig,
	srv *http.Server,
	sm *session.Manager,
	activity *serverActivity,
) (bool, error) {
	listeners, err := systemdListeners()
	if err != nil {
		return true, fmt.Errorf("load socket activation listeners: %w", err)
	}
	if len(listeners) == 0 {
		return false, nil
	}

	if err := configureServerTLS(srv); err != nil {
		closeListeners(listeners)
		return true, err
	}

	var stopOnce sync.Once
	servStopped := make(chan struct{})
	var serveErr error
	stop := func(err error) {
		stopOnce.Do(func() {
			serveErr = err
			close(servStopped)
		})
	}

	for _, listener := range listeners {
		go serveTLSListener(srv, web.NewTLSRedirectListener(listener, srv.TLSConfig, cfg.Port), stop, "server error (TLS)")
	}
	slog.Info("socket-activated HTTPS server listening")
	startSocketIdleExitWatcher(
		srv, sm, activity,
		90*time.Second, 15*time.Second,
	)

	<-servStopped
	return true, serveErr
}

func serveWithSelfBind(cfg ServerConfig, srv *http.Server) error {
	if err := configureServerTLS(srv); err != nil {
		return err
	}

	listener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		slog.Error("listen failed", "address", srv.Addr, "error", err)
		return fmt.Errorf("listen %s: %w", srv.Addr, err)
	}
	tlsListener := web.NewTLSRedirectListener(listener, srv.TLSConfig, cfg.Port)
	slog.Info("HTTPS server listening", "address", fmt.Sprintf("https://localhost:%d", cfg.Port))
	if err := srv.Serve(tlsListener); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		return fmt.Errorf("serve HTTPS: %w", err)
	}
	return nil
}

func configureServerTLS(srv *http.Server) error {
	certificateDir := filepath.Join(version.WebserverStateDir, "certificates")
	cert, err := web.LoadOrCreateSelfSignedCert(certificateDir)
	if err != nil {
		slog.Error("failed to load managed certificate", "directory", certificateDir, "error", err)
		return fmt.Errorf("load managed TLS certificate: %w", err)
	}
	srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	return nil
}

func serveTLSListener(srv *http.Server, listener net.Listener, stop func(error), errorPrefix string) {
	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		slog.Error(errorPrefix, "error", err)
		stop(fmt.Errorf("%s: %w", errorPrefix, err))
		return
	}
	stop(nil)
}

func waitForServerShutdown(quit <-chan os.Signal, done <-chan error) error {
	select {
	case <-quit:
		slog.Info("shutdown signal received")
		return nil
	case err := <-done:
		slog.Info("HTTP server stopped, beginning shutdown")
		return err
	}
}

func shutdownHTTPServer(srv *http.Server) {
	srv.SetKeepAlivesEnabled(false)

	ctx, cancel := shutdownContext()
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
	activity *serverActivity,
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
			if !activity.idleFor(idleGrace) {
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
			ctx, cancel := shutdownContext()
			if err := srv.Shutdown(ctx); err != nil {
				slog.Warn("idle shutdown failed", "idle_grace", idleGrace, "error", err)
			}
			cancel()
			return
		}
	}()
}

func shutdownContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}
