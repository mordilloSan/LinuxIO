package updates

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
)

func getAutoUpdatesHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)

	out, err := bridge.CallWithSession(sess, "dbus", "GetAutoUpdates", nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", out)
}

func putAutoUpdatesHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)

	var opts AutoUpdateOptions
	if err := c.ShouldBindJSON(&opts); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}

	// Bridge expects a single JSON string argument.
	argBytes, _ := json.Marshal(opts)
	args := []string{string(argBytes)}

	out, err := bridge.CallWithSession(sess, "dbus", "SetAutoUpdates", args)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", out)
}

func postApplyOfflineUpdatesHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)

	out, err := bridge.CallWithSession(sess, "dbus", "ApplyOfflineUpdates", nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", out)
}
