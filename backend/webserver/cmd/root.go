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
	// -------------------------------------------------------------------------
	// Logging (from flags)
	// -------------------------------------------------------------------------
	verbose := cfg.Verbose

	// Configure log levels based on verbose flag
	var levels []logger.Level
	if verbose {
		levels = logger.AllLevels() // Includes DEBUG
	} else {
		levels = []logger.Level{logger.InfoLevel, logger.WarnLevel, logger.ErrorLevel}
	}

	logger.Init(logger.Config{
		Levels: levels,
	})
	logger.InfoKV("server starting", "verbose", verbose)

	// -------------------------------------------------------------------------
	// Sessions + cleanup hooks
	// -------------------------------------------------------------------------
	ms := session.New()
	sm := session.NewManager(ms, session.SessionConfig{
		Cookie: session.CookieConfig{
			Secure: true,
		},
	})
	sm.RegisterOnDelete(func(sess *session.Session, reason session.DeleteReason) {
		if sess.User.Username == "" {
			return
		}
		// Close yamux session (bridge connection)
		bridge.CloseYamuxSession(sess.SessionID)
		// Close WebSocket connection (frontend connection)
		web.CloseWebSocketForSession(sess.SessionID)
	})

	// -------------------------------------------------------------------------
	// Frontend assets
	// -------------------------------------------------------------------------
	ui, err := web.UI()
	if err != nil {
		logger.Errorf("failed to mount embedded frontend: %v", err)
		os.Exit(1)
	}

	// -------------------------------------------------------------------------
	// Router
	// -------------------------------------------------------------------------
	router := web.BuildRouter(web.Config{
		Verbose: verbose,
		UI:      ui,
		RegisterRoutes: func(mux *http.ServeMux) {
			auth.RegisterAuthRoutes(mux, sm, verbose)
		},
	}, sm)

	// -------------------------------------------------------------------------
	// Request tracking for idle-exit
	// -------------------------------------------------------------------------
	var inFlight atomic.Int64
	var lastHit atomic.Int64
	lastHit.Store(time.Now().UnixNano())

	// Wrap router with request tracking middleware
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastHit.Store(time.Now().UnixNano())
		inFlight.Add(1)
		defer inFlight.Add(-1)
		router.ServeHTTP(w, r)
	})

	// -------------------------------------------------------------------------
	// HTTP(S) server
	// -------------------------------------------------------------------------
	srv := &http.Server{
		Handler:  handler,
		ErrorLog: log.New(web.HTTPErrorLogAdapter{}, "", 0),
	}

	quit := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// self-bind (dev and local prod)
	addr := fmt.Sprintf(":%d", cfg.Port)
	srv.Addr = addr

	go func() {
		var err error
		// -------- systemd socket activation first ----------
		listeners, actErr := activation.Listeners()
		if actErr != nil {
			logger.Warnf("activation.Listeners error: %v", actErr)
		}
		if len(listeners) > 0 {
			var stopOnce sync.Once
			servStopped := make(chan struct{})
			stop := func() { stopOnce.Do(func() { close(servStopped) }) }

			cert, cErr := web.GenerateSelfSignedCert()
			if cErr != nil {
				logger.Errorf("Failed to generate cert: %v", cErr)
			}
			srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}

			for _, l := range listeners {
				tlsLis := tls.NewListener(l, srv.TLSConfig)
				go func(lis net.Listener) {
					if e := srv.Serve(lis); e != nil && e != http.ErrServerClosed {
						logger.Errorf("server error (TLS): %v", e)
						os.Exit(1)
					}
					stop()
				}(tlsLis)
			}
			logger.Infof("Socket-activated HTTPS server listening on inherited sockets")

			// Start idle-exit only in socket-activation mode
			const idleGrace = 90 * time.Second
			const checkEvery = 15 * time.Second
			startSocketIdleExitWatcher(
				srv, sm, &inFlight, &lastHit,
				idleGrace, checkEvery,
				func(msg string, args ...any) { logger.Infof(msg, args...) },
			)

			// Block until Serve() exits (due to Shutdown or error)
			<-servStopped
			close(done)
			return
		}

		// -------- fallback: self-bind (manual runs) ----------
		addr := fmt.Sprintf(":%d", cfg.Port)
		srv.Addr = addr

		cert, cErr := web.GenerateSelfSignedCert()
		if cErr != nil {
			logger.Errorf("Failed to generate cert: %v", cErr)
			os.Exit(1)
		}
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}

		logger.Infof("HTTPS server (self-bound) at https://localhost:%d", cfg.Port)
		err = srv.ListenAndServeTLS("", "")

		if err != nil && err != http.ErrServerClosed {
			logger.Errorf("server error: %v", err)
			os.Exit(1)
		}
		close(done)
	}()

	// -------------------------------------------------------------------------
	// Shutdown coordination
	// -------------------------------------------------------------------------
	select {
	case <-quit:
		logger.Infof("🛑 Shutdown signal received")
	case <-done:
		logger.Infof("HTTP server stopped, beginning shutdown...")
	}

	srv.SetKeepAlivesEnabled(false)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		if errors.Is(err, context.DeadlineExceeded) {
			logger.Warnf("Graceful HTTP shutdown timed out; forcing close of remaining connections.")
			if cerr := srv.Close(); cerr != nil && !errors.Is(cerr, http.ErrServerClosed) {
				logger.Warnf("HTTP server force-close error: %v", cerr)
			}
		} else {
			logger.Warnf("HTTP server shutdown error: %v", err)
		}
	} else {
		logger.Infof("HTTP server closed")
	}

	// Close yamux sessions to trigger bridge shutdown
	if sessions, err := sm.ActiveSessions(); err == nil {
		for _, sess := range sessions {
			bridge.CloseYamuxSession(sess.SessionID)
		}
	}

	// Close sessions
	sm.Close()

	fmt.Println("Server stopped.")
	logger.Infof("Server stopped.")
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
			_ = srv.Shutdown(ctx)
			cancel()
			return
		}
	}()
}
