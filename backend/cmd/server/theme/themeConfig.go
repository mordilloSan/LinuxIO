package theme

import (
	"net/http"
	"os/user"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/cmd/server/auth"
	"github.com/mordilloSan/LinuxIO/internal/config"
	"github.com/mordilloSan/LinuxIO/internal/logger"
)

// Reuse the struct from internal/config to avoid drift.
type ThemeSettings = config.ThemeSettings

// Public: return defaults (use for the login screen, before we know the user)
func registerPublicThemeRoutes(r *gin.Engine) {
	r.GET("/theme/default", func(c *gin.Context) {
		// Any base is fine for theme-only defaults; use the current OS user's home.
		u, _ := user.Current()
		base := ""
		if u != nil && u.HomeDir != "" {
			base = u.HomeDir
		}
		def := config.DefaultSettings(base).ThemeSettings
		c.JSON(http.StatusOK, def)
	})
}

// Authenticated: read/write the logged-in user's theme
func registerPrivateThemeRoutes(r *gin.Engine) {
	group := r.Group("/theme", auth.AuthMiddleware())

	// GET /theme/get -> user's saved theme (bridge has already ensured the file)
	group.GET("/get", func(c *gin.Context) {
		username := usernameFromContext(c)
		if username == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "no session user"})
			return
		}
		cfg, _, err := config.Load(username)
		if err != nil {
			logger.Warnf("Load theme for %s failed: %v", username, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load theme"})
			return
		}
		c.JSON(http.StatusOK, cfg.ThemeSettings)
	})

	// POST /theme/set -> update user's theme in the unified config file
	group.POST("/set", func(c *gin.Context) {
		username := usernameFromContext(c)
		if username == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "no session user"})
			return
		}

		var body ThemeSettings
		if err := c.BindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		// Validate
		if body.Theme != "LIGHT" && body.Theme != "DARK" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid theme value"})
			return
		}
		if !config.IsValidCSSColor(body.PrimaryColor) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid primaryColor"})
			return
		}

		// Load → update → save
		cfg, _, err := config.Load(username)
		if err != nil {
			logger.Warnf("Load before save for %s failed: %v", username, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load settings"})
			return
		}
		cfg.ThemeSettings = body

		if _, err := config.Save(username, cfg); err != nil {
			logger.Warnf("Save theme for %s failed: %v", username, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save theme"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "theme updated"})
	})
}

// RegisterThemeRoutes wires both public (defaults) and private (user) routes.
func RegisterThemeRoutes(router *gin.Engine) {
	registerPublicThemeRoutes(router)  // for login page
	registerPrivateThemeRoutes(router) // for authenticated UI
}

// Helper: extract the session username placed by your auth middleware.
// Adjust if your auth package exposes a helper; this version tries common keys.
func usernameFromContext(c *gin.Context) string {
	// common keys your middleware may set; adjust to your actual values
	if v, ok := c.Get("user"); ok {
		// string username
		if s, ok := v.(string); ok && s != "" {
			return s
		}
		// struct { ID string } or utils.User with ID
		type withID interface{ GetID() string }
		if w, ok := v.(withID); ok && w.GetID() != "" {
			return w.GetID()
		}
		// fallback to map-like
		if m, ok := v.(map[string]any); ok {
			if s, ok := m["id"].(string); ok && s != "" {
				return s
			}
			if s, ok := m["name"].(string); ok && s != "" {
				return s
			}
		}
	}
	// last resort (not ideal, but avoids panic if something is misconfigured)
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return ""
}
