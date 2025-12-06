package updates

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
)

func getAutoUpdatesHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)

	var raw json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "dbus", "GetAutoUpdates", nil, &raw); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", raw)
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

	var raw json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "dbus", "SetAutoUpdates", args, &raw); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", raw)
}

func postApplyOfflineUpdatesHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)

	var raw json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "dbus", "ApplyOfflineUpdates", nil, &raw); err != nil {
		if errors.Is(err, bridge.ErrEmptyBridgeOutput) {
			c.Data(http.StatusOK, "application/json", []byte(`{"status":"ok"}`))
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", raw)
}
