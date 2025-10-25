package config

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
)

func RegisterThemeRoutes(priv *gin.RouterGroup) {
	// GET /theme/get -> user's saved theme settings
	priv.GET("/get", func(c *gin.Context) {
		sess := session.SessionFromContext(c)
		if sess == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "no session"})
			return
		}

		data, err := bridge.CallWithSession(sess, "config", "theme_get", []string{sess.User.Username})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load theme"})
			return
		}

		var resp ipc.Response
		if err := json.Unmarshal(data, &resp); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
			return
		}
		if resp.Status != "ok" {
			errMsg := strings.TrimPrefix(resp.Error, "bad_request:")
			status := http.StatusInternalServerError
			if strings.HasPrefix(resp.Error, "bad_request:") {
				status = http.StatusBadRequest
			}
			c.JSON(status, gin.H{"error": errMsg})
			return
		}
		if resp.Output == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
			return
		}
		c.JSON(http.StatusOK, resp.Output) // Gin will marshal it
	})

	// POST /theme/set -> update user's theme (and related UI settings)
	priv.POST("/set", func(c *gin.Context) {
		sess := session.SessionFromContext(c)
		if sess == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "no session"})
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
			return
		}

		data, err := bridge.CallWithSession(sess, "config", "theme_set", []string{sess.User.Username, string(body)})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save theme"})
			return
		}

		var resp ipc.Response
		if err := json.Unmarshal(data, &resp); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
			return
		}
		if resp.Status != "ok" {
			errMsg := strings.TrimPrefix(resp.Error, "bad_request:")
			status := http.StatusInternalServerError
			if strings.HasPrefix(resp.Error, "bad_request:") {
				status = http.StatusBadRequest
			}
			c.JSON(status, gin.H{"error": errMsg})
			return
		}
		if resp.Output == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
			return
		}
		c.JSON(http.StatusOK, resp.Output) // Gin will marshal it
	})
}
