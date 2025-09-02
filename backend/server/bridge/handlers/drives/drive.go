package drives

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/LinuxIO/internal/ipc"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
	"github.com/mordilloSan/LinuxIO/server/bridge"
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

	c.Data(http.StatusOK, "application/json", resp.Output)
}
