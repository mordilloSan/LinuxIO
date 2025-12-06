package system

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
)

func handleGetFastUpdates(c *gin.Context) {
	sess := session.SessionFromContext(c)
	var resp json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "system", "get_updates_fast", nil, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed", "detail": err.Error()})
		return
	}
	if len(resp) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(http.StatusOK, resp)
}
