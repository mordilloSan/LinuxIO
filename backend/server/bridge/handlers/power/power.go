package power

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/LinuxIO/common/logger"
	"github.com/mordilloSan/LinuxIO/common/session"
	"github.com/mordilloSan/LinuxIO/server/bridge"
)

// RegisterPowerRoutes mounts power actions on a pre-authenticated group.
func RegisterPowerRoutes(group *gin.RouterGroup) {
	group.POST("/reboot", func(c *gin.Context) {
		sess := session.SessionFromContext(c)
		output, err := bridge.CallWithSession(sess, "dbus", "Reboot", nil)
		if err != nil {
			logger.Errorf("Reboot failed: %+v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":  "reboot failed",
				"detail": err.Error(),
				"output": output,
			})
			return
		}
		logger.Infof("Reboot triggered successfully for user %s (session: %s)", sess.User.Username, sess.SessionID)
		c.JSON(http.StatusOK, gin.H{"message": "rebooting...", "output": output})
	})

	group.POST("/shutdown", func(c *gin.Context) {
		sess := session.SessionFromContext(c)
		output, err := bridge.CallWithSession(sess, "dbus", "PowerOff", nil)
		if err != nil {
			logger.Errorf("Shutdown failed: %+v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":  "shutdown failed",
				"detail": err.Error(),
				"output": output,
			})
			return
		}
		logger.Infof("Shutdown triggered successfully for user %s (session: %s)", sess.User.Username, sess.SessionID)
		c.JSON(http.StatusOK, gin.H{"message": "shutting down...", "output": output})
	})
}
