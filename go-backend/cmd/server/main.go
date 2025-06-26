package main

import (
	"crypto/tls"
	embed "go-backend"
	"go-backend/cmd/server/docker"
	"go-backend/internal/auth"
	"go-backend/internal/benchmark"
	"go-backend/internal/dockers"
	"go-backend/internal/filebrowser"
	"go-backend/internal/logger"
	"go-backend/internal/networks"
	"go-backend/internal/power"
	"go-backend/internal/services"
	"go-backend/internal/session"
	"go-backend/internal/system"
	"go-backend/internal/templates"
	"go-backend/internal/theme"
	"go-backend/internal/updates"

	"go-backend/internal/utils"
	"go-backend/internal/websocket"
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
	logger.Init("env", verbose)
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

	router := gin.New()
	router.Use(gin.Recovery())

	if env == "development" {
		router.SetTrustedProxies(nil)
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

	// Reverse Proxy for filebrowser
	router.Any("/navigator/*proxyPath", auth.AuthMiddleware(), filebrowser.FilebrowserReverseProxy(filebrowserSecret))

	// start docker micro services
	go docker.StartServices(filebrowserSecret)

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
	os.Setenv("SERVER_PORT", port)

	// Start the server
	addr := ":" + port

	if env == "production" {
		cert, err := utils.GenerateSelfSignedCert()
		if err != nil {
			logger.Error.Fatalf("❌ Failed to generate self-signed certificate: %v", err)
		}

		srv := &http.Server{
			Addr:      addr,
			Handler:   router,
			TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
		}
		logger.Infof("🚀 Server running at https://localhost:%s", port)
		logger.Error.Fatal(srv.ListenAndServeTLS("", "")) // Empty filenames = use TLSConfig.Certificates
	} else {
		logger.Infof("🚀 Server running at http://localhost:%s", port)
		logger.Error.Fatal(router.Run(addr))
	}

}
