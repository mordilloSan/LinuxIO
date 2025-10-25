package cmd

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/activation"
	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
	"github.com/mordilloSan/LinuxIO/backend/server/cleanup"
	"github.com/mordilloSan/LinuxIO/backend/server/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/server/web"
	"github.com/mordilloSan/go_logger/logger"
)

func RunServer(cfg ServerConfig) {
	// -------------------------------------------------------------------------
	// Env + logging (from flags)
	// -------------------------------------------------------------------------
	env := strings.ToLower(cfg.Env) // "development" | "production"
	verbose := cfg.Verbose
	logger.Init(env, verbose)
	if !(env == "development" && verbose) {
		gin.SetMode(gin.ReleaseMode)
	}
	logger.InfoKV("server starting", "env", env, "verbose", verbose)

	// -------------------------------------------------------------------------
	// Sessions + cleanup hooks
	// -------------------------------------------------------------------------
	ms := session.New()
	sm := session.NewManager(ms, session.SessionConfig{
		Cookie: session.CookieConfig{
			Secure: env == "production",
		},
	})
	sm.RegisterOnDelete(func(sess session.Session, reason session.DeleteReason) {
		if sess.User.Username != "" {
			if _, err := bridge.CallWithSession(&sess, "control", "shutdown", []string{string(reason)}); err != nil {
				logger.WarnKV("bridge shutdown failed", "user", sess.User.Username, "reason", reason, "error", err)
			}
		}
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
	// Start required services
	// -------------------------------------------------------------------------
	filebrowserSecret := utils.GenerateSecretKey(32)
	go func() {
		filebrowser.StartServices(filebrowserSecret, verbose, env == "development")
	}()

	// -------------------------------------------------------------------------
	// Router
	// -------------------------------------------------------------------------
	router := BuildRouter(Config{
		Env:               env,
		Verbose:           verbose,
		VitePort:          cfg.ViteDevPort,
		FilebrowserSecret: filebrowserSecret,
		UI:                ui,
	}, sm)

	// -------------------------------------------------------------------------asdasd
	// -------------------------------------------------------------------------
	var inFlight atomic.Int64
	var lastHit atomic.Int64
	lastHit.Store(time.Now().UnixNano())

	router.Use(func(c *gin.Context) {
		lastHit.Store(time.Now().UnixNano())
		inFlight.Add(1)
		defer inFlight.Add(-1)
		c.Next()
	})

	// -------------------------------------------------------------------------
	// HTTP(S) server
	// -------------------------------------------------------------------------
	srv := &http.Server{
		Handler:  router,
		ErrorLog: log.New(HTTPErrorLogAdapter{}, "", 0),
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

			if strings.ToLower(cfg.Env) == "production" {
				cert, cErr := web.GenerateSelfSignedCert()
				if cErr != nil {
					logger.Errorf("Failed to generate cert: %v", cErr)
				}
				srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
				web.SetRootPoolFromServerCert(cert)

				// Export base URL and cert for children (bridge, etc.)
				_ = os.Setenv("LINUXIO_SERVER_BASE_URL", "https://localhost:8090")
				if len(cert.Certificate) > 0 {
					pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})
					if len(pemBytes) > 0 {
						_ = os.Setenv("LINUXIO_SERVER_CERT", string(pemBytes))
					}
				}

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
			} else {
				// Plain HTTP for dev
				for _, l := range listeners {
					go func(lis net.Listener) {
						if e := srv.Serve(lis); e != nil && e != http.ErrServerClosed {
							logger.Errorf("server error: %v", e)
							os.Exit(1)
						}
						stop()
					}(l)
				}
				logger.Infof("Socket-activated HTTP server listening on inherited sockets")
			}

			// 🔻 Start idle-exit only in socket-activation mode
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

		// -------- fallback: self-bind (dev / manual runs) ----------
		addr := fmt.Sprintf(":%d", cfg.Port)
		srv.Addr = addr
		if strings.ToLower(cfg.Env) == "production" {
			cert, cErr := web.GenerateSelfSignedCert()
			if cErr != nil {
				logger.Errorf("Failed to generate cert: %v", cErr)
				os.Exit(1)
			}
			srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
			web.SetRootPoolFromServerCert(cert)

			baseURL := fmt.Sprintf("https://localhost:%d", cfg.Port)
			_ = os.Setenv("LINUXIO_SERVER_BASE_URL", baseURL)
			if len(cert.Certificate) > 0 {
				pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})
				if len(pemBytes) > 0 {
					_ = os.Setenv("LINUXIO_SERVER_CERT", string(pemBytes))
				}
			}

			logger.Infof("HTTPS server (self-bound) at %s", baseURL)
			err = srv.ListenAndServeTLS("", "")
		} else {
			baseURL := fmt.Sprintf("http://localhost:%d", cfg.Port)
			_ = os.Setenv("LINUXIO_SERVER_BASE_URL", baseURL)
			logger.Infof("HTTP server (self-bound) at %s", baseURL)
			err = srv.ListenAndServe()
		}

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

	// Stop background/attached services
	cleanup.CleanupFilebrowserContainer(env == "development")

	// Tell bridges to quit before sessions close
	cleanup.ShutdownAllBridges(sm, "server_quit")

	// Close sessions
	sm.Close()

	if env == "production" {
		fmt.Println("Server stopped.")
	}
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
