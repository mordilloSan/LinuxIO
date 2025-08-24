package web

import (
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/LinuxIO/cmd/server/auth"
	"github.com/mordilloSan/LinuxIO/cmd/server/benchmark"
	"github.com/mordilloSan/LinuxIO/cmd/server/config"
	"github.com/mordilloSan/LinuxIO/cmd/server/docker"
	"github.com/mordilloSan/LinuxIO/cmd/server/network"
	"github.com/mordilloSan/LinuxIO/cmd/server/power"
	"github.com/mordilloSan/LinuxIO/cmd/server/services"
	"github.com/mordilloSan/LinuxIO/cmd/server/system"
	"github.com/mordilloSan/LinuxIO/cmd/server/updates"
	"github.com/mordilloSan/LinuxIO/cmd/server/websocket"
	"github.com/mordilloSan/LinuxIO/cmd/server/wireguard"
	"github.com/mordilloSan/LinuxIO/internal/logger"
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
		r.Use(auth.CorsMiddleware(cfg.VitePort))
		if cfg.Verbose {
			r.Use(gin.Logger())
		}
	}

	// Auth + APIs
	auth.RegisterAuthRoutes(r, auth.Config{
		Env:                  cfg.Env,
		Verbose:              cfg.Verbose,
		BridgeBinaryOverride: cfg.BridgeBinaryOverride,
	})
	system.RegisterSystemRoutes(r)
	updates.RegisterUpdateRoutes(r)
	services.RegisterServiceRoutes(r)
	network.RegisterNetworkRoutes(r)
	docker.RegisterDockerRoutes(r)
	config.RegisterThemeRoutes(r)
	power.RegisterPowerRoutes(r)
	wireguard.RegisterWireguardRoutes(r)

	// WebSocket
	r.GET("/ws", websocket.WebSocketHandler)

	// Filebrowser (auth protected)
	r.Any("/navigator/*proxyPath", auth.AuthMiddleware(), NavigatorDefaultsMiddleware(), auth.FilebrowserReverseProxy(cfg.FilebrowserSecret))

	// Debug-only routes
	if cfg.Env != "production" {
		benchmark.RegisterDebugRoutes(r, cfg.Env)
	}

	// Frontend (SPA)
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

// httpErrorLogAdapter forwards http.Server errors into logger,
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
