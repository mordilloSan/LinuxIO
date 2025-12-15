package config

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
)

func RegisterThemeRoutes(priv *gin.RouterGroup) {
	// GET /theme/get -> user's saved theme settings
	priv.GET("/get", func(c *gin.Context) {
		sess := session.SessionFromContext(c)
		if sess == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
			return
		}

		var result json.RawMessage
		if err := bridge.CallTypedWithSession(sess, "config", "theme_get", []string{sess.User.Username}, &result); err != nil {
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "bad_request:") {
				status = http.StatusBadRequest
			}
			errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
			errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
			c.JSON(status, gin.H{"error": errMsg})
			return
		}
		c.JSON(http.StatusOK, result)
	})

	// POST /theme/set -> update user's theme (and related UI settings)
	priv.POST("/set", func(c *gin.Context) {
		sess := session.SessionFromContext(c)
		if sess == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
			return
		}

		var result json.RawMessage
		if err := bridge.CallTypedWithSession(sess, "config", "theme_set", []string{sess.User.Username, string(body)}, &result); err != nil {
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "bad_request:") {
				status = http.StatusBadRequest
			}
			errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
			errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
			c.JSON(status, gin.H{"error": errMsg})
			return
		}
		c.JSON(http.StatusOK, result)
	})
}
