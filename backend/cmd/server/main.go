package main

import (
	"context"
	"crypto/tls"
	"embed"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/LinuxIO/cmd/server/auth"
	"github.com/mordilloSan/LinuxIO/cmd/server/benchmark"
	"github.com/mordilloSan/LinuxIO/cmd/server/cleanup"
	docker "github.com/mordilloSan/LinuxIO/cmd/server/docker"
	"github.com/mordilloSan/LinuxIO/cmd/server/filebrowser"
	"github.com/mordilloSan/LinuxIO/cmd/server/network"
	"github.com/mordilloSan/LinuxIO/cmd/server/power"
	"github.com/mordilloSan/LinuxIO/cmd/server/services"
	"github.com/mordilloSan/LinuxIO/cmd/server/system"
	"github.com/mordilloSan/LinuxIO/cmd/server/templates"
	"github.com/mordilloSan/LinuxIO/cmd/server/theme"
	"github.com/mordilloSan/LinuxIO/cmd/server/updates"
	"github.com/mordilloSan/LinuxIO/cmd/server/websocket"
	"github.com/mordilloSan/LinuxIO/cmd/server/wireguard"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
	"github.com/mordilloSan/LinuxIO/internal/utils"
	"github.com/spf13/pflag"
)

//go:embed all:frontend/assets/*
var StaticFS embed.FS

//go:embed all:frontend/.vite/manifest.json
var ViteManifest []byte

//go:embed all:frontend/manifest.json all:frontend/favicon-*.png
var PWAManifest embed.FS

func main() {
	var env string
	var verbose bool
	var port int
	var vitePort int
	var bridgeOverride string

	pflag.StringVar(&env, "env", "production", "environment (development|production)")
	pflag.BoolVar(&verbose, "verbose", false, "enable verbose logs") // presence-only: --verbose
	pflag.IntVar(&port, "port", 8080, "HTTP server port")
	pflag.IntVar(&vitePort, "vite-port", 5173, "Vite dev server port (dev only)")
	pflag.StringVar(&bridgeOverride, "bridge-binary", "", "path to linuxio-bridge (optional)")
	pflag.Parse()
	env = strings.ToLower(env)

	logger.Init(env, verbose)
	if !verbose {
		gin.SetMode(gin.ReleaseMode)
	}
	logger.Infof("🌱 Starting server in %s mode...", env)

	// Sessions (Init starts actor + GC). Choose policy & timeouts.
	shutdownSessions, err := session.Init(&session.Config{
		IdleTimeout:          30 * time.Minute, // sliding
		AbsoluteTimeout:      6 * time.Hour,    // cap (matches your sessionDuration)
		RefreshInterval:      60 * time.Second, // throttled refresh
		SingleSessionPerUser: false,            // flip to true if you want exclusive login
		GCInterval:           10 * time.Minute,
	})
	if err != nil {
		logger.Error.Fatalf("session init failed: %v", err)
	}
	defer shutdownSessions()

	// Background services
	network.StartSimpleNetInfoSampler()
	system.InitGPUInfo()

	// FileBrowser
	filebrowserSecret := utils.GenerateSecretKey(32)
	go filebrowser.StartServices(filebrowserSecret)

	// Router
	router := gin.New()
	router.Use(gin.Recovery())

	if env == "development" {
		if err := router.SetTrustedProxies(nil); err != nil {
			logger.Warnf("failed to set trusted proxies: %v", err)
		}
		router.Use(auth.CorsMiddleware(vitePort))
		if verbose {
			router.Use(gin.Logger())
		}
	}

	// Auth routes (inject config for bridge launching + cookies)
	auth.RegisterAuthRoutes(router, auth.Config{
		Env:                  env,
		Verbose:              verbose,
		BridgeBinaryOverride: bridgeOverride,
	})

	// Backend routes
	system.RegisterSystemRoutes(router)
	updates.RegisterUpdateRoutes(router)
	services.RegisterServiceRoutes(router)
	network.RegisterNetworkRoutes(router)
	docker.RegisterDockerRoutes(router)
	theme.RegisterThemeRoutes(router)
	power.RegisterPowerRoutes(router)
	wireguard.RegisterWireguardRoutes(router)

	// Reverse Proxy for filebrowser
	router.Any("/navigator/*proxyPath", auth.AuthMiddleware(), auth.FilebrowserReverseProxy(filebrowserSecret))

	// Debug/benchmark routes
	if env != "production" {
		benchmark.RegisterDebugRoutes(router, env)
	}

	// Static files (prod)
	if env == "production" {
		templates.RegisterStaticRoutes(router, StaticFS, PWAManifest)
	}

	// WebSocket
	router.GET("/ws", websocket.WebSocketHandler)

	// Frontend index + SPA fallback (pass vite dev port from flag)
	router.GET("/", func(c *gin.Context) {
		templates.ServeIndex(c, env, vitePort, ViteManifest)
	})
	router.NoRoute(func(c *gin.Context) {
		templates.ServeIndex(c, env, vitePort, ViteManifest)
	})

	// HTTP server
	addr := ":" + /* int to string */ fmt.Sprintf("%d", port)
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	// Route stdlib http server errors to our logger instead of stderr
	srv.ErrorLog = log.New(httpErrorLogAdapter{}, "", 0)

	if env == "production" {
		cert, err := utils.GenerateSelfSignedCert()
		if err != nil {
			logger.Error.Fatalf("❌ Failed to generate self-signed certificate: %v", err)
		}
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	}

	// Graceful shutdown coordination
	quit := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		var err error
		if env == "production" {
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

	// Wait for signal or unexpected stop
	select {
	case <-quit:
		logger.Infof("🛑 Shutdown signal received, shutting down server...")
	case <-done:
		logger.Infof("🚨 Server stopped unexpectedly, beginning shutdown...")
	}

	// Shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil && err != http.ErrServerClosed {
		logger.Error.Fatalf("❌ Server forced to shutdown: %v", err)
	}

	// Cleanup
	if err := cleanup.CleanupFilebrowserContainer(); err != nil {
		logger.Warnf("FileBrowser cleanup error: %v", err)
	}
	cleanup.ShutdownAllBridges("server_quit")
	logger.Infof("Server stopped.")
}

// httpErrorLogAdapter implements io.Writer and forwards http.Server errors into our logger.
// We silently drop the noisy "TLS handshake error ... EOF" entries.
type httpErrorLogAdapter struct{}

func (httpErrorLogAdapter) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if msg == "" {
		return len(p), nil
	}
	if strings.Contains(msg, "TLS handshake error") && strings.Contains(msg, "EOF") {
		// keep it quiet; still visible at debug if you want:
		logger.Debugf("[http.Server suppressed] %s", msg)
		return len(p), nil
	}
	// Everything else goes through your logger (journald in prod)
	logger.Warnf("[http.Server] %s", msg)
	return len(p), nil
}
