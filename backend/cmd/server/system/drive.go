package system

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/LinuxIO/cmd/server/bridge"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
)

func getDiskInfo(c *gin.Context) {
	sess := session.GetSessionOrAbort(c)
	if sess == nil {
		return
	}

	output, err := bridge.CallWithSession(sess, "system", "get_drive_info", nil)
	if err != nil {
		logger.Errorf("Failed to get drive info via bridge: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var resp struct {
		Status string          `json:"status"`
		Output json.RawMessage `json:"output"`
		Error  string          `json:"error"`
	}
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
