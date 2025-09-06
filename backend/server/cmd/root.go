package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/common/logger"
	"github.com/mordilloSan/LinuxIO/common/session"
	"github.com/mordilloSan/LinuxIO/common/utils"
	"github.com/mordilloSan/LinuxIO/server/api"
	"github.com/mordilloSan/LinuxIO/server/bridge"
	"github.com/mordilloSan/LinuxIO/server/cleanup"
	"github.com/mordilloSan/LinuxIO/server/filebrowser"
	"github.com/mordilloSan/LinuxIO/server/terminal"
	"github.com/mordilloSan/LinuxIO/server/web"
)

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func intFromEnv(key string, def int) int {
	if s := os.Getenv(key); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n < 65536 {
			return n
		}
	}
	return def
}

func RunServer(cfg ServerConfig) {
	// ----------------------------------------------------------------------------
	// Env + logging
	// ----------------------------------------------------------------------------
	env := strings.ToLower(envOrDefault("LINUXIO_ENV", "production"))
	verbose := strings.EqualFold(os.Getenv("LINUXIO_VERBOSE"), "1") ||
		strings.EqualFold(os.Getenv("LINUXIO_VERBOSE"), "true")
	logger.Init(env, verbose)
	if !(env == "development" && verbose) {
		gin.SetMode(gin.ReleaseMode)
	}
	logger.Infof("🌱 Starting server in %s mode...", env)

	// ----------------------------------------------------------------------------
	// Sessions  + cleanup hooks
	// ----------------------------------------------------------------------------
	ms := session.New()
	sm := session.NewManager(ms, session.SessionConfig{
		Cookie: session.CookieConfig{
			Secure: env == "production", // keep true when TLS-terminated upstream
		},
	})

	// Fan-out all session deletions (bridge+terminal cleanup)
	sm.RegisterOnDelete(func(sess session.Session, reason session.DeleteReason) {
		// 1) Close all terminals for this session
		terminal.CloseAllForSession(sess.SessionID)
		// 2) Politely ask bridge to shutdown (best-effort)
		if sess.User.Username != "" {
			if _, err := bridge.CallWithSession(&sess, "control", "shutdown", []string{string(reason)}); err != nil {
				logger.Warnf("Bridge shutdown for %s failed: %v", sess.SessionID, err)
			}
		}
	})

	// ----------------------------------------------------------------------------
	// Background samplers, GPU info
	// ----------------------------------------------------------------------------
	api.StartSimpleNetInfoSampler()
	api.InitGPUInfo()

	// ----------------------------------------------------------------------------
	// Frontend assets
	// ----------------------------------------------------------------------------
	ui, err := web.UI()
	if err != nil {
		logger.Error.Fatalf("failed to mount embedded frontend: %v", err)
	}

	// ----------------------------------------------------------------------------
	// Start required services
	// ----------------------------------------------------------------------------
	filebrowserSecret := utils.GenerateSecretKey(32)
	filebrowser.StartServices(filebrowserSecret, verbose)

	// ----------------------------------------------------------------------------
	// Router
	// ----------------------------------------------------------------------------
	router := BuildRouter(Config{
		Env:                  env,
		Verbose:              verbose,
		VitePort:             intFromEnv("VITE_DEV_PORT", 3000),
		BridgeBinaryOverride: cfg.BridgeBinaryPath,
		FilebrowserSecret:    filebrowserSecret,
		UI:                   ui,
	}, sm)

	// ----------------------------------------------------------------------------
	// HTTP(S) server
	// ----------------------------------------------------------------------------
	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:     addr,
		Handler:  router,
		ErrorLog: log.New(HTTPErrorLogAdapter{}, "", 0),
	}

	if env == "production" {
		cert, err := web.GenerateSelfSignedCert()
		if err != nil {
			logger.Error.Fatalf("❌ Failed to generate self-signed certificate: %v", err)
		}
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
		web.SetRootPoolFromServerCert(cert)
	}

	quit := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start HTTP server AFTER services are ready
	go func() {
		var err error
		if env == "production" {
			fmt.Printf("🚀 Server running at https://localhost:%d\n", cfg.Port)
			logger.Infof("HTTP server listening at https://localhost:%d", cfg.Port)
			err = srv.ListenAndServeTLS("", "")
		} else {
			logger.Infof("HTTP server listening at http://localhost:%d", cfg.Port)
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			logger.Error.Fatalf("server error: %v", err)
		}
		close(done)
	}()

	// ----------------------------------------------------------------------------
	// Shutdown coordination
	// ----------------------------------------------------------------------------
	select {
	case <-quit:
		logger.Infof("🛑 Shutdown signal received")
	case <-done:
		logger.Infof("🚨 HTTP server stopped unexpectedly, beginning shutdown...")
	}

	// Graceful -> forced shutdown
	srv.SetKeepAlivesEnabled(false)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		if errors.Is(err, context.DeadlineExceeded) {
			logger.Warnf("⏳ Graceful HTTP shutdown timed out; forcing close of remaining connections.")
			if cerr := srv.Close(); cerr != nil && !errors.Is(cerr, http.ErrServerClosed) {
				logger.Warnf("HTTP server force-close error: %v", cerr)
			}
		} else {
			logger.Warnf("HTTP server shutdown error: %v", err)
		}
	} else {
		logger.Infof("HTTP server clossed")
	}

	// Stop background/attached services
	cleanup.CleanupFilebrowserContainer()

	// Tell bridges to quit before sessions close
	cleanup.ShutdownAllBridges(sm, "server_quit")

	// Close sessions
	sm.Close()

	if env == "production" {
		fmt.Println("Server stopped.")
	}
	logger.Infof("Server stopped.")
}
