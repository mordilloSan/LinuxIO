package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/activation"
	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/webserver/auth"
	"github.com/mordilloSan/LinuxIO/backend/webserver/bridge"
	"github.com/mordilloSan/LinuxIO/backend/webserver/web"
)

func RunServer(cfg ServerConfig) {
	initServerLogger(cfg.Verbose)
	logger.InfoKV("server starting", "verbose", cfg.Verbose)

	sm := newSessionManager()
	srv, inFlight, lastHit := newHTTPServer(cfg, sm)
	quit, done := startHTTPServer(cfg, srv, sm, inFlight, lastHit)

	waitForServerShutdown(quit, done)
	shutdownHTTPServer(srv)
	closeBridgeSessions(sm)
	sm.Close()
	logger.Infof("Server stopped.")
}

func initServerLogger(verbose bool) {
	var levels []logger.Level
	if verbose {
		levels = logger.AllLevels()
	} else {
		levels = []logger.Level{logger.InfoLevel, logger.WarnLevel, logger.ErrorLevel}
	}
	logger.Init(logger.Config{
		Levels:           levels,
		IncludeCallerTag: true,
	})
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
		logger.Errorf("failed to mount embedded frontend: %v", err)
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
	listeners, err := activation.Listeners()
	if err != nil {
		logger.Warnf("activation.Listeners error: %v", err)
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

	logger.Infof("Socket-activated HTTPS server listening on inherited sockets")
	startSocketIdleExitWatcher(
		srv, sm, inFlight, lastHit,
		90*time.Second, 15*time.Second,
		func(msg string, args ...any) { logger.Infof(msg, args...) },
	)

	<-servStopped
	close(done)
	return true
}

func serveWithSelfBind(cfg ServerConfig, srv *http.Server, done chan struct{}) {
	configureServerTLS(srv)

	listener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		logger.Errorf("listen error: %v", err)
		os.Exit(1)
	}
	tlsListener := web.NewTLSRedirectListener(listener, srv.TLSConfig, cfg.Port)

	logger.Infof("HTTPS server (self-bound) at https://localhost:%d", cfg.Port)
	if err := srv.Serve(tlsListener); err != nil && err != http.ErrServerClosed {
		logger.Errorf("server error: %v", err)
		os.Exit(1)
	}
	close(done)
}

func configureServerTLS(srv *http.Server) {
	cert, err := web.GenerateSelfSignedCert()
	if err != nil {
		logger.Errorf("Failed to generate cert: %v", err)
		os.Exit(1)
	}
	srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
}

func serveTLSListener(srv *http.Server, listener net.Listener, stop func(), errorPrefix string) {
	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		logger.Errorf("%s: %v", errorPrefix, err)
		os.Exit(1)
	}
	stop()
}

func waitForServerShutdown(quit <-chan os.Signal, done <-chan struct{}) {
	select {
	case <-quit:
		logger.Infof(" Shutdown signal received")
	case <-done:
		logger.Infof("HTTP server stopped, beginning shutdown...")
	}
}

func shutdownHTTPServer(srv *http.Server) {
	srv.SetKeepAlivesEnabled(false)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		if errors.Is(err, context.DeadlineExceeded) {
			logger.Warnf("Graceful HTTP shutdown timed out; forcing close of remaining connections.")
			if cerr := srv.Close(); cerr != nil && !errors.Is(cerr, http.ErrServerClosed) {
				logger.Warnf("HTTP server force-close error: %v", cerr)
			}
			return
		}
		logger.Warnf("HTTP server shutdown error: %v", err)
		return
	}
	logger.Infof("HTTP server closed")
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
	logf func(string, ...any),
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

			logf("Idle for %v and no active sessions — exiting (socket will keep the port open)", idleGrace)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := srv.Shutdown(ctx); err != nil {
				logf("Idle shutdown failed: %v", err)
			}
			cancel()
			return
		}
	}()
}
