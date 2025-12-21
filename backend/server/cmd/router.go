package cmd

import (
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/go_logger/logger"

	appconfig "github.com/mordilloSan/LinuxIO/backend/common/config"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/auth"
	"github.com/mordilloSan/LinuxIO/backend/server/benchmark"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge/handlers/control"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge/handlers/filebrowser"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge/handlers/network"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge/handlers/wireguard"
	"github.com/mordilloSan/LinuxIO/backend/server/web"
)

type Config struct {
	Env                  string
	Verbose              bool
	VitePort             int
	BridgeBinaryOverride string
	UI                   fs.FS
}

func BuildRouter(cfg Config, sm *session.Manager) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	if cfg.Env == appconfig.EnvDevelopment {
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
	authPrivate.Use(sm.RequireSession())

	auth.RegisterAuthRoutes(authPublic, authPrivate, sm, auth.Config{
		Env:                  cfg.Env,
		Verbose:              cfg.Verbose,
		BridgeBinaryOverride: cfg.BridgeBinaryOverride,
	})

	// Protected endpoints:
	network.RegisterNetworkRoutes(r.Group("/network", sm.RequireSession()))
	wireguard.RegisterWireguardRoutes(r.Group("/wireguard", sm.RequireSession()))
	config.RegisterThemeRoutes(r.Group("/theme", sm.RequireSession()))
	control.RegisterControlRoutes(r.Group("/control", sm.RequireSession()))

	navigator := r.Group("/navigator", sm.RequireSession())
	if err := filebrowser.RegisterRoutes(navigator); err != nil {
		logger.Errorf("failed to register filebrowser routes: %v", err)
		navigator.Any("/*path", func(c *gin.Context) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "file browser unavailable"})
		})
	}

	// --- WebSocket ---
	r.GET("/ws", sm.RequireSession(), web.WebSocketHandler)
	r.GET("/ws/relay", sm.RequireSession(), web.WebSocketRelayHandler)

	// --- Benchmark in dev mode ---
	if cfg.Env != appconfig.EnvProduction {
		benchmark.RegisterDebugRoutes(r, cfg.Env, sm)
	}

	// --- Frontend (SPA) ---
	if cfg.Env == appconfig.EnvDevelopment {
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
	r.GET("/favicon-4.png", func(c *gin.Context) { serveFileFS(c, ui, "favicon-4.png") })
	r.GET("/favicon-3.png", func(c *gin.Context) { serveFileFS(c, ui, "favicon-3.png") })
	r.GET("/favicon-2.png", func(c *gin.Context) { serveFileFS(c, ui, "favicon-2.png") })
	r.GET("/favicon-1.png", func(c *gin.Context) { serveFileFS(c, ui, "favicon-1.png") })

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
	if strings.Contains(msg, "TLS handshake error") && strings.Contains(msg, "unknown certificate") {
		// Browsers commonly abort on self-signed certs; not actionable noise.
		logger.Debugf("[http.Server suppressed] %s", msg)
		return len(p), nil
	}
	logger.Warnf("[http.Server] %s", msg)
	return len(p), nil
}
