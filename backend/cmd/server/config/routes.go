package config

import (
	"net/http"
	"os/user"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
)

// Payload with pointer fields so we can detect what the client actually sent.
// Accept both "sidebarCollapsed" (canonical) and "SidebarCollapsed" (legacy).
type appSettingsPayload struct {
	Theme               *string `json:"theme"`
	PrimaryColor        *string `json:"primaryColor"`
	SidebarCollapsed    *bool   `json:"sidebarCollapsed"`
	SidebarCollapsedAlt *bool   `json:"SidebarCollapsed"`
}

// RegisterThemeRoutes wires the private (user) routes under /theme.
//
// IMPORTANT: The provided group MUST already include your auth middleware.
// Example caller:
//
//	priv := r.Group("/theme", web.AuthMiddleware())
//	config.RegisterThemeRoutes(priv)
func RegisterThemeRoutes(priv *gin.RouterGroup) {
	// GET /theme/get -> user's saved theme settings
	priv.GET("/get", func(c *gin.Context) {
		username := usernameFromContext(c)
		if username == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "no session user"})
			return
		}

		cfg, cfgPath, err := Load(username)
		if err != nil {
			logger.Warnf("[theme.get] user=%q load failed: %v", username, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load theme"})
			return
		}

		logger.Debugf("[theme.get] user=%q path=%s theme=%s primary=%s collapsed=%v",
			username, cfgPath, cfg.AppSettings.Theme, cfg.AppSettings.PrimaryColor, cfg.AppSettings.SidebarCollapsed)

		// Respond with the canonical struct (camelCase JSON tags)
		c.JSON(http.StatusOK, cfg.AppSettings)
	})

	// POST /theme/set -> update user's theme (and related UI settings)
	priv.POST("/set", func(c *gin.Context) {
		username := usernameFromContext(c)
		if username == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "no session user"})
			return
		}

		var p appSettingsPayload
		if err := c.BindJSON(&p); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}

		// Load current settings
		cfg, _, err := Load(username)
		if err != nil {
			logger.Warnf("[theme.set] user=%q load-before-save failed: %v", username, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load settings"})
			return
		}
		prev := cfg.AppSettings
		next := prev // start with previous, override only provided fields

		// Theme (normalize + validate) if provided
		if p.Theme != nil {
			t := strings.ToUpper(strings.TrimSpace(*p.Theme))
			if t != "LIGHT" && t != "DARK" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid theme value (LIGHT|DARK)"})
				return
			}
			next.Theme = t
		}

		// Primary color if provided
		if p.PrimaryColor != nil {
			if !IsValidCSSColor(*p.PrimaryColor) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid primaryColor"})
				return
			}
			next.PrimaryColor = *p.PrimaryColor
		}

		// Sidebar collapsed (prefer canonical camelCase)
		if p.SidebarCollapsed != nil {
			next.SidebarCollapsed = *p.SidebarCollapsed
		} else if p.SidebarCollapsedAlt != nil {
			next.SidebarCollapsed = *p.SidebarCollapsedAlt
		}

		cfg.AppSettings = next

		// Save
		cfgPath, err := Save(username, cfg)
		if err != nil {
			logger.Warnf("[theme.set] user=%q save failed: %v", username, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save theme"})
			return
		}

		// Verify
		verifyCfg, verifyPath, vErr := Load(username)
		ok := (vErr == nil &&
			verifyCfg.AppSettings.Theme == next.Theme &&
			verifyCfg.AppSettings.PrimaryColor == next.PrimaryColor &&
			verifyCfg.AppSettings.SidebarCollapsed == next.SidebarCollapsed)

		if vErr != nil {
			logger.Warnf("[theme.set] user=%q verify-load failed: %v (path=%s)", username, vErr, verifyPath)
		}

		logger.Debugf("[theme.set] user=%q path=%s prev={%s,%s,%v} new={%s,%s,%v} verify=%v",
			username, cfgPath,
			prev.Theme, prev.PrimaryColor, prev.SidebarCollapsed,
			next.Theme, next.PrimaryColor, next.SidebarCollapsed, ok,
		)

		c.JSON(http.StatusOK, gin.H{
			"message":          "theme updated",
			"verify":           ok,
			"path":             cfgPath,
			"appliedTheme":     next.Theme,
			"appliedPrimary":   next.PrimaryColor,
			"sidebarCollapsed": next.SidebarCollapsed,
		})
	})
}

// Helper: extract the session username placed by your auth middleware.
func usernameFromContext(c *gin.Context) string {
	// Preferred: our auth middleware sets "session" with *session.Session
	if v, ok := c.Get("session"); ok {
		if s, ok := v.(*session.Session); ok && s != nil && s.User.Username != "" {
			return s.User.Username
		}
	}

	// Fallbacks (legacy cases)
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

	// Last resort: current OS user (dev)
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return ""
}
