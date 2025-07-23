package main

import (
	embed "backend"
	"backend/cmd/server/filebrowser"
	"backend/internal/auth"
	"backend/internal/benchmark"
	"backend/internal/dockers"
	"backend/internal/logger"
	"backend/internal/networks"
	"backend/internal/power"
	"backend/internal/services"
	"backend/internal/session"
	"backend/internal/system"
	"backend/internal/templates"
	"backend/internal/theme"
	"backend/internal/updates"
	"backend/internal/utils"
	"backend/internal/websocket"
	"backend/internal/wireguard"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

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
	networks.RegisterNetworkRoutes(router)
	dockers.RegisterDockerRoutes(router)
	dockers.RegisterDockerComposeRoutes(router)
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
		templates.RegisterStaticRoutes(router, embed.StaticFS, embed.PWAManifest)
	}

	// WebSocket route
	router.GET("/ws", websocket.WebSocketHandler)

	// ✅ Serve frontend on "/" and fallback routes
	router.GET("/", func(c *gin.Context) {
		templates.ServeIndex(c, env, embed.ViteManifest)
	})
	router.NoRoute(func(c *gin.Context) {
		templates.ServeIndex(c, env, embed.ViteManifest)
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
	if env == "production" {
		cert, err := utils.GenerateSelfSignedCert()
		if err != nil {
			logger.Error.Fatalf("❌ Failed to generate self-signed certificate: %v", err)
		}
		fmt.Printf("🚀 Server running at https://localhost:%s\n", port)
		srv := &http.Server{
			Addr:      addr,
			Handler:   router,
			TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
			ErrorLog:  log.New(io.Discard, "", 0),
		}
		logger.Error.Fatal(srv.ListenAndServeTLS("", ""))
	} else {
		fmt.Printf("🚀 Server running at http://localhost:%s\n", port)
		logger.Error.Fatal(router.Run(addr))
	}
}
