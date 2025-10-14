package drives

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/logger"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
)

func getDiskInfo(c *gin.Context) {
	sess := session.SessionFromContext(c)

	output, err := bridge.CallWithSession(sess, "system", "get_drive_info", nil)
	if err != nil {
		logger.Errorf("Failed to get drive info via bridge: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		logger.Errorf("Failed to decode bridge response: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "decode bridge response"})
		return
	}

	if resp.Status != "ok" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}

	if resp.Output == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(http.StatusOK, resp.Output) // Changed: Gin will marshal it
}
