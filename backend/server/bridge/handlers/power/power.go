package power

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
)

// RegisterPowerRoutes mounts power actions on a pre-authenticated group.
func RegisterPowerRoutes(group *gin.RouterGroup) {
	group.POST("/reboot", func(c *gin.Context) {
		sess := session.SessionFromContext(c)
		var resp json.RawMessage
		if err := bridge.CallTypedWithSession(sess, "dbus", "Reboot", nil, &resp); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":  "reboot failed",
				"detail": err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "rebooting...", "output": resp})
	})

	group.POST("/shutdown", func(c *gin.Context) {
		sess := session.SessionFromContext(c)
		var resp json.RawMessage
		if err := bridge.CallTypedWithSession(sess, "dbus", "PowerOff", nil, &resp); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":  "shutdown failed",
				"detail": err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "shutting down...", "output": resp})
	})
}
