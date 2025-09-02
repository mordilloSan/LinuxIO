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

	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
	"github.com/mordilloSan/LinuxIO/internal/utils"
	"github.com/mordilloSan/LinuxIO/server/api"
	"github.com/mordilloSan/LinuxIO/server/cleanup"
	"github.com/mordilloSan/LinuxIO/server/filebrowser"
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
	// Optional: keep env/verbose behavior via ENV VARS (no CLI flags).
	env := strings.ToLower(envOrDefault("LINUXIO_ENV", "production"))
	verbose := strings.EqualFold(os.Getenv("LINUXIO_VERBOSE"), "1") ||
		strings.EqualFold(os.Getenv("LINUXIO_VERBOSE"), "true")

	logger.Init(env, verbose)
	if !(env == "development" && verbose) {
		gin.SetMode(gin.ReleaseMode)
	}
	logger.Infof("🌱 Starting server in %s mode...", env)

	// Sessions Cleanup
	defer session.Init()()

	// API startup for caching
	api.StartSimpleNetInfoSampler()
	api.InitGPUInfo()

	// FileBrowser
	filebrowserSecret := utils.GenerateSecretKey(32)
	go filebrowser.StartServices(filebrowserSecret, verbose)

	// Frontend FS
	ui, err := web.UI()
	if err != nil {
		logger.Error.Fatalf("failed to mount embedded frontend: %v", err)
	}

	router := BuildRouter(Config{
		Env:                  env,
		Verbose:              verbose,
		VitePort:             intFromEnv("VITE_DEV_PORT", 3000),
		BridgeBinaryOverride: cfg.BridgeBinaryPath,
		FilebrowserSecret:    filebrowserSecret,
		UI:                   ui,
	})

	// HTTP(S) server
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

	go func() {
		var err error
		if env == "production" {
			fmt.Printf("🚀 Server running at https://localhost:%d\n", cfg.Port)
			logger.Infof("🚀 Server running at https://localhost:%d", cfg.Port)
			err = srv.ListenAndServeTLS("", "")
		} else {
			logger.Infof("🚀 Server running at http://localhost:%d", cfg.Port)
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			logger.Error.Fatalf("server error: %v", err)
		}
		close(done)
	}()

	select {
	case <-quit:
		if env == "production" {
			fmt.Println("🛑 Shutdown signal received, shutting down server...")
		}
		logger.Infof("🛑 Shutdown signal received, shutting down server...")
	case <-done:
		if env == "production" {
			fmt.Println("🚨 Server stopped unexpectedly, beginning shutdown...")
		}
		logger.Infof("🚨 Server stopped unexpectedly, beginning shutdown...")
	}

	// graceful -> forced shutdown
	srv.SetKeepAlivesEnabled(false)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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
	}

	// Cleanup
	cleanup.ShutdownAllBridges("server_quit")
	if err := cleanup.CleanupFilebrowserContainer(); err != nil {
		logger.Warnf("FileBrowser cleanup error: %v", err)
	}

	if env == "production" {
		fmt.Println("Server stopped.")
	}
	logger.Infof("Server stopped.")
}
