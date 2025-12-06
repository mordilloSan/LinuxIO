package drives

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
)

func getDiskInfo(c *gin.Context) {
	sess := session.SessionFromContext(c)

	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "system", "get_drive_info", nil, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}
