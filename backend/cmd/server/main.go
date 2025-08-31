package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/pflag"

	"github.com/mordilloSan/LinuxIO/cmd/server/api"
	"github.com/mordilloSan/LinuxIO/cmd/server/cleanup"
	"github.com/mordilloSan/LinuxIO/cmd/server/filebrowser"
	"github.com/mordilloSan/LinuxIO/cmd/server/web"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
	"github.com/mordilloSan/LinuxIO/internal/utils"
)

//go:embed all:web/frontend/*
var FrontendFS embed.FS

func main() {
	var env string
	var verbose bool
	var port int
	var vitePort int
	var bridgeOverride string

	pflag.StringVar(&env, "env", "production", "environment (development|production)")
	pflag.BoolVar(&verbose, "verbose", false, "enable verbose logs")
	pflag.IntVar(&port, "port", 8080, "HTTP server port")
	pflag.IntVar(&vitePort, "vite-port", 5173, "Vite dev server port (dev only)")
	pflag.StringVar(&bridgeOverride, "bridge-binary", "", "path to linuxio-bridge (optional)")
	pflag.Parse()
	env = strings.ToLower(env)

	logger.Init(env, verbose)
	if !(env == "development" && verbose) {
		gin.SetMode(gin.ReleaseMode)
	}
	logger.Infof("🌱 Starting server in %s mode...", env)

	// Sessions Cleanup
	defer session.Init()()

	// API startup for chaching
	api.StartSimpleNetInfoSampler()
	api.InitGPUInfo()

	// FileBrowser
	filebrowserSecret := utils.GenerateSecretKey(32)
	go filebrowser.StartServices(filebrowserSecret, verbose)

	// Sub FS rooted at the build directory ("web/frontend")
	ui, err := fs.Sub(FrontendFS, "web/frontend")
	if err != nil {
		logger.Error.Fatalf("failed to mount embedded frontend: %v", err)
	}

	// Build router
	router := web.BuildRouter(web.Config{
		Env:                  env,
		Verbose:              verbose,
		VitePort:             vitePort,
		BridgeBinaryOverride: bridgeOverride,
		FilebrowserSecret:    filebrowserSecret,
		UI:                   ui,
	})

	// HTTP(S) server
	addr := ":" + fmt.Sprintf("%d", port)
	srv := &http.Server{
		Addr:     addr,
		Handler:  router,
		ErrorLog: log.New(web.HTTPErrorLogAdapter{}, "", 0),
	}

	if env == "production" {
		cert, err := web.GenerateSelfSignedCert()
		if err != nil {
			logger.Error.Fatalf("❌ Failed to generate self-signed certificate: %v", err)
		}
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}

		// Seed the *auth* package’s client trust pool used by syncFilebrowser
		if err := SetTrustedPoolFromServerCert(cert); err != nil {
			logger.Error.Fatalf("❌ Failed to set trusted pool: %v", err)
		}
	}

	quit := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		var err error
		if env == "production" {
			fmt.Printf("🚀 Server running at https://localhost:%d\n", port)
			logger.Infof("🚀 Server running at https://localhost:%d", port)
			err = srv.ListenAndServeTLS("", "")
		} else {
			logger.Infof("🚀 Server running at http://localhost:%d", port)
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

	// === graceful -> forced shutdown sequence ===
	srv.SetKeepAlivesEnabled(false) // stop new keep-alive requests

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

	// Cleanup (signal bridges first so they cancel work promptly)
	cleanup.ShutdownAllBridges("server_quit")

	if err := cleanup.CleanupFilebrowserContainer(); err != nil {
		logger.Warnf("FileBrowser cleanup error: %v", err)
	}

	if env == "production" {
		fmt.Println("Server stopped.")
	}
	logger.Infof("Server stopped.")
}

var TrustedRootPool *x509.CertPool

func SetTrustedPoolFromServerCert(tc tls.Certificate) error {
	if len(tc.Certificate) == 0 {
		return fmt.Errorf("no certificate bytes in tls.Certificate")
	}
	leaf, err := x509.ParseCertificate(tc.Certificate[0]) // DER -> *x509.Certificate
	if err != nil {
		return fmt.Errorf("parse leaf cert: %w", err)
	}
	p := x509.NewCertPool()
	p.AddCert(leaf)
	TrustedRootPool = p
	return nil
}
