package power

import (
	"github.com/mordilloSan/LinuxIO/backend/cmd/server/auth"
	"github.com/mordilloSan/LinuxIO/backend/internal/bridge"
	"github.com/mordilloSan/LinuxIO/backend/internal/logger"
	"github.com/mordilloSan/LinuxIO/backend/internal/session"
	"net/http"

	"github.com/gin-gonic/gin"
)

func RegisterPowerRoutes(r *gin.Engine) {
	group := r.Group("/power", auth.AuthMiddleware())

	group.POST("/reboot", func(c *gin.Context) {
		sess := session.GetSessionOrAbort(c)
		if sess == nil {
			return
		}
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
		logger.Infof("Reboot triggered successfully for user %s (session: %s)", sess.User.ID, sess.SessionID)
		c.JSON(http.StatusOK, gin.H{"message": "rebooting...", "output": output})
	})

	group.POST("/shutdown", func(c *gin.Context) {
		sess := session.GetSessionOrAbort(c)
		if sess == nil {
			return
		}
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
		logger.Infof("Shutdown triggered successfully for user %s (session: %s)", sess.User.ID, sess.SessionID)
		c.JSON(http.StatusOK, gin.H{"message": "shutting down...", "output": output})
	})
}
