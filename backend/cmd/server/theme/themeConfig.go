package theme

import (
	"net/http"
	"os/user"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/cmd/server/auth"
	"github.com/mordilloSan/LinuxIO/internal/config"
	"github.com/mordilloSan/LinuxIO/internal/logger"
)

// Reuse the struct from internal/config to avoid drift.
type ThemeSettings = config.ThemeSettings

// RegisterThemeRoutes wires the private (user) routes.
func RegisterThemeRoutes(router *gin.Engine) {
	registerPrivateThemeRoutes(router)
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

		cfg, cfgPath, err := config.Load(username)
		if err != nil {
			logger.Warnf("[theme.get] user=%q load failed: %v", username, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load theme"})
			return
		}

		logger.Infof("[theme.get] user=%q path=%s theme=%s primary=%s",
			username, cfgPath, cfg.ThemeSettings.Theme, cfg.ThemeSettings.PrimaryColor)

		// Normal response: just the theme settings
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

		// Normalize & validate early (accept common lowercase from UI)
		body.Theme = strings.ToUpper(strings.TrimSpace(body.Theme))
		if body.Theme != "LIGHT" && body.Theme != "DARK" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid theme value (LIGHT|DARK)"})
			return
		}
		if !config.IsValidCSSColor(body.PrimaryColor) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid primaryColor"})
			return
		}

		// Load current -> update -> save
		cfg, _, err := config.Load(username)
		if err != nil {
			logger.Warnf("[theme.set] user=%q load-before-save failed: %v", username, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load settings"})
			return
		}

		prev := cfg.ThemeSettings
		cfg.ThemeSettings = body

		cfgPath, err := config.Save(username, cfg)
		if err != nil {
			logger.Warnf("[theme.set] user=%q save failed: %v", username, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save theme"})
			return
		}

		// Re-load to verify persistence
		verifyCfg, verifyPath, vErr := config.Load(username)
		ok := (vErr == nil &&
			verifyCfg.ThemeSettings.Theme == body.Theme &&
			verifyCfg.ThemeSettings.PrimaryColor == body.PrimaryColor)

		if vErr != nil {
			logger.Warnf("[theme.set] user=%q verify-load failed: %v (path=%s)", username, vErr, verifyPath)
		}

		logger.Infof("[theme.set] user=%q path=%s prev={%s,%s} new={%s,%s} verify=%v",
			username, cfgPath,
			prev.Theme, prev.PrimaryColor,
			body.Theme, body.PrimaryColor, ok,
		)

		// Return compact info; client can ignore extras. (Useful while debugging.)
		c.JSON(http.StatusOK, gin.H{
			"message":        "theme updated",
			"verify":         ok,
			"path":           cfgPath,
			"appliedTheme":   body.Theme,
			"appliedPrimary": body.PrimaryColor,
		})
	})
}

// Helper: extract the session username placed by your auth middleware.
// Adjust if your auth package exposes a helper; this version tries common keys.
func usernameFromContext(c *gin.Context) string {
	if v, ok := c.Get("user"); ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
		type withID interface{ GetID() string }
		if w, ok := v.(withID); ok && w.GetID() != "" {
			return w.GetID()
		}
		if m, ok := v.(map[string]any); ok {
			if s, ok := m["id"].(string); ok && s != "" {
				return s
			}
			if s, ok := m["name"].(string); ok && s != "" {
				return s
			}
		}
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return ""
}
