package config

import (
	"net/http"
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

func RegisterThemeRoutes(priv *gin.RouterGroup) {
	// GET /theme/get -> user's saved theme settings
	priv.GET("/get", func(c *gin.Context) {
		sess := session.SessionFromContext(c)
		if sess == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "no session"})
			return
		}

		cfg, cfgPath, err := Load(sess.User.Username)
		if err != nil {
			logger.Warnf("[theme.get] user=%q load failed: %v", sess.User.Username, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load theme"})
			return
		}

		logger.Debugf("[theme.get] user=%q path=%s theme=%s primary=%s collapsed=%v",
			sess.User.Username, cfgPath, cfg.AppSettings.Theme, cfg.AppSettings.PrimaryColor, cfg.AppSettings.SidebarCollapsed)

		c.JSON(http.StatusOK, cfg.AppSettings)
	})

	// POST /theme/set -> update user's theme (and related UI settings)
	priv.POST("/set", func(c *gin.Context) {
		sess := session.SessionFromContext(c)
		if sess == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "no session"})
			return
		}

		var p appSettingsPayload
		if err := c.BindJSON(&p); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}

		// Load current settings
		cfg, _, err := Load(sess.User.Username)
		if err != nil {
			logger.Warnf("[theme.set] user=%q load-before-save failed: %v", sess.User.Username, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load settings"})
			return
		}
		prev := cfg.AppSettings
		next := prev

		// Apply overrides...
		if p.Theme != nil {
			t := strings.ToUpper(strings.TrimSpace(*p.Theme))
			if t != "LIGHT" && t != "DARK" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid theme value (LIGHT|DARK)"})
				return
			}
			next.Theme = t
		}
		if p.PrimaryColor != nil {
			if !IsValidCSSColor(*p.PrimaryColor) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid primaryColor"})
				return
			}
			next.PrimaryColor = *p.PrimaryColor
		}
		if p.SidebarCollapsed != nil {
			next.SidebarCollapsed = *p.SidebarCollapsed
		} else if p.SidebarCollapsedAlt != nil {
			next.SidebarCollapsed = *p.SidebarCollapsedAlt
		}

		cfg.AppSettings = next
		cfgPath, err := Save(sess.User.Username, cfg)
		if err != nil {
			logger.Warnf("[theme.set] user=%q save failed: %v", sess.User.Username, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save theme"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":          "theme updated",
			"path":             cfgPath,
			"appliedTheme":     next.Theme,
			"appliedPrimary":   next.PrimaryColor,
			"sidebarCollapsed": next.SidebarCollapsed,
		})
	})
}
