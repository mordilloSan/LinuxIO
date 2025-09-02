package cmd

import (
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/server/api"
	"github.com/mordilloSan/LinuxIO/server/auth"
	"github.com/mordilloSan/LinuxIO/server/benchmark"
	"github.com/mordilloSan/LinuxIO/server/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/server/bridge/handlers/drives"
	"github.com/mordilloSan/LinuxIO/server/bridge/handlers/network"
	"github.com/mordilloSan/LinuxIO/server/bridge/handlers/power"
	"github.com/mordilloSan/LinuxIO/server/bridge/handlers/services"
	"github.com/mordilloSan/LinuxIO/server/bridge/handlers/updates"
	"github.com/mordilloSan/LinuxIO/server/bridge/handlers/wireguard"
	"github.com/mordilloSan/LinuxIO/server/config"
	"github.com/mordilloSan/LinuxIO/server/web"
)

type Config struct {
	Env                  string
	Verbose              bool
	VitePort             int
	BridgeBinaryOverride string
	FilebrowserSecret    string
	UI                   fs.FS
}

func BuildRouter(cfg Config) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	if cfg.Env == "development" {
		if err := r.SetTrustedProxies(nil); err != nil {
			logger.Warnf("failed to set trusted proxies: %v", err)
		}
		r.Use(web.CorsMiddleware(cfg.VitePort))
		if cfg.Verbose {
			r.Use(gin.Logger())
		}
	}

	// --- Auth routes ---
	authPublic := r.Group("/auth")
	authPrivate := r.Group("/auth")
	authPrivate.Use(web.AuthMiddleware())

	auth.RegisterAuthRoutes(authPublic, authPrivate, auth.Config{
		Env:                  cfg.Env,
		Verbose:              cfg.Verbose,
		BridgeBinaryOverride: cfg.BridgeBinaryOverride,
	})

	// --- APIs ---
	api.RegisterSystemRoutes(r.Group("/system")) //We want a public API just for get methods....
	updates.RegisterUpdateRoutes(r.Group("/updates", web.AuthMiddleware()))
	services.RegisterServiceRoutes(r.Group("/services", web.AuthMiddleware()))
	network.RegisterNetworkRoutes(r.Group("/network", web.AuthMiddleware()))
	docker.RegisterDockerRoutes(r.Group("/docker", web.AuthMiddleware()))
	drives.RegisterDriveRoutes(r.Group("/drives", web.AuthMiddleware()))
	power.RegisterPowerRoutes(r.Group("/power", web.AuthMiddleware()))
	wireguard.RegisterWireguardRoutes(r.Group("/wireguard", web.AuthMiddleware()))
	config.RegisterThemeRoutes(r.Group("/theme", web.AuthMiddleware()))

	// --- WebSocket ---
	r.GET("/ws", web.WebSocketHandler)

	// --- Filebrowser (auth protected) ---
	r.Any("/navigator/*proxyPath", web.AuthMiddleware(), web.FilebrowserReverseProxy(cfg.FilebrowserSecret))

	// --- Benchmark in dev mode ---
	if cfg.Env != "production" {
		benchmark.RegisterDebugRoutes(r, cfg.Env)
	}

	// --- Frontend (SPA) ---
	if cfg.Env == "development" {
		r.GET("/", func(c *gin.Context) {
			c.Redirect(http.StatusTemporaryRedirect, fmt.Sprintf("http://localhost:%d/", cfg.VitePort))
		})
		r.NoRoute(func(c *gin.Context) {
			target := fmt.Sprintf("http://localhost:%d%s", cfg.VitePort, c.Request.URL.RequestURI())
			c.Redirect(http.StatusTemporaryRedirect, target)
		})
	} else {
		mountProductionSPA(r, cfg.UI)
	}

	return r
}

func mountProductionSPA(r *gin.Engine, ui fs.FS) {
	// /assets/* (bundled JS/CSS/images)
	if assets, err := fs.Sub(ui, "assets"); err == nil {
		r.StaticFS("/assets", http.FS(assets))
	}

	// PWA/static files if present
	r.GET("/manifest.json", func(c *gin.Context) { serveFileFS(c, ui, "manifest.json") })
	r.GET("/favicon.ico", func(c *gin.Context) { serveFileFS(c, ui, "favicon.ico") })
	r.GET("/favicon-5.png", func(c *gin.Context) { serveFileFS(c, ui, "favicon-5.png") })

	// Root + SPA fallback
	r.GET("/", func(c *gin.Context) { serveFileFS(c, ui, "index.html") })
	r.NoRoute(func(c *gin.Context) { serveFileFS(c, ui, "index.html") })
}

func serveFileFS(c *gin.Context, fsys fs.FS, path string) {
	b, err := fs.ReadFile(fsys, path)
	if err != nil {
		c.String(http.StatusNotFound, "%s not found", path)
		return
	}
	ct := mime.TypeByExtension(filepath.Ext(path))
	if ct == "" {
		ct = "text/html; charset=utf-8"
	}
	c.Data(http.StatusOK, ct, b)
}

// httpErrorLogAdapter forwards http.Server errors into logger
type HTTPErrorLogAdapter struct{}

func (HTTPErrorLogAdapter) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if msg == "" {
		return len(p), nil
	}
	if strings.Contains(msg, "TLS handshake error") && strings.Contains(msg, "EOF") {
		logger.Debugf("[http.Server suppressed] %s", msg)
		return len(p), nil
	}
	logger.Warnf("[http.Server] %s", msg)
	return len(p), nil
}
