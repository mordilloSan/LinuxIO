package main

import (
	"context"
	"crypto/tls"
	"embed"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
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
)

//go:embed all:frontend/assets/*
var StaticFS embed.FS

//go:embed all:frontend/.vite/manifest.json
var ViteManifest []byte

//go:embed all:frontend/manifest.json all:frontend/favicon-*.png
var PWAManifest embed.FS

func main() {
	_ = godotenv.Load("../.env")
	var env = os.Getenv("GO_ENV")
	if env == "" {
		env = "production"
	}
	verbose := os.Getenv("VERBOSE") == "true"
	logger.Init(env, verbose)
	if !verbose {
		gin.SetMode(gin.ReleaseMode)
	}

	logger.Infof("🌱 Starting server in %s mode...", env)

	// Start the session garbage collector
	session.StartSessionGC()
	// Start the network interface sampler
	network.StartSimpleNetInfoSampler()
	// Initialize cache functions
	system.InitGPUInfo()
	// Generate Secret Key
	filebrowserSecret := utils.GenerateSecretKey(32)
	// Start FileBrowser
	go filebrowser.StartServices(filebrowserSecret)
	router := gin.New()
	router.Use(gin.Recovery())

	if env == "development" {
		if err := router.SetTrustedProxies(nil); err != nil {
			logger.Warnf("failed to set trusted proxies: %v", err)
		}
		router.Use(auth.CorsMiddleware())
		router.Use(gin.Logger())
	}

	// Backend routes
	auth.RegisterAuthRoutes(router)
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
	// API Benchmark route
	if env != "production" {
		benchmark.RegisterDebugRoutes(router, env)
	}

	// Static files (only needed in production if files exist on disk)
	if env == "production" {
		templates.RegisterStaticRoutes(router, StaticFS, PWAManifest)
	}

	// WebSocket route
	router.GET("/ws", websocket.WebSocketHandler)

	// ✅ Serve frontend on "/" and fallback routes
	router.GET("/", func(c *gin.Context) {
		templates.ServeIndex(c, env, ViteManifest)
	})
	router.NoRoute(func(c *gin.Context) {
		templates.ServeIndex(c, env, ViteManifest)
	})

	// Port config
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8080"
		logger.Warnf("⚠️  SERVER_PORT not set, defaulting to 8080")
	}
	if err := os.Setenv("SERVER_PORT", port); err != nil {
		logger.Warnf("failed to set SERVER_PORT: %v", err)
	}

	// Start the server
	addr := ":" + port
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}
	if env == "production" {
		cert, err := utils.GenerateSelfSignedCert()
		if err != nil {
			logger.Error.Fatalf("❌ Failed to generate self-signed certificate: %v", err)
		}
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	}

	// --- Graceful shutdown with robust server done channel ---
	quit := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		var err error
		if env == "production" {
			fmt.Printf("🚀 Server running at https://localhost:%s\n", port)
			err = srv.ListenAndServeTLS("", "")
		} else {
			fmt.Printf("🚀 Server running at http://localhost:%s\n", port)
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			logger.Error.Fatalf("server error: %v", err)
		}
		close(done) // Always close the done channel, even for clean shutdown
	}()

	// Wait for either signal or server stop
	select {
	case <-quit:
		logger.Infof("🛑 Shutdown signal received, shutting down server...")
	case <-done:
		logger.Infof("🚨 Server stopped unexpectedly, beginning shutdown...")
	}

	// Graceful shutdown context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil && err != http.ErrServerClosed {
		logger.Error.Fatalf("❌ Server forced to shutdown: %v", err)
	}

	// --- Cleanup FileBrowser Docker container on shutdown
	if err := cleanup.CleanupFilebrowserContainer(); err != nil {
		logger.Warnf("FileBrowser cleanup error: %v", err)
	}
	logger.Infof("✅ Server gracefully stopped")
}
