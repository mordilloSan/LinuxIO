package system

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/mem"

	"github.com/mordilloSan/LinuxIO/common/ipc"
	"github.com/mordilloSan/LinuxIO/common/logger"
	"github.com/mordilloSan/LinuxIO/common/session"
	"github.com/mordilloSan/LinuxIO/server/bridge"
)

type MemoryResponse struct {
	System *mem.VirtualMemoryStat `json:"system"`
	Docker struct {
		Used uint64 `json:"used"`
	} `json:"docker"`
	ZFS struct {
		ARC uint64 `json:"arc"`
	} `json:"zfs"`
}

func handleGetMemory(c *gin.Context) {
	sess := session.SessionFromContext(c)
	logger.Infof("%s requested Memory info (session: %s)", sess.User.Username, sess.SessionID)

	rawResp, err := bridge.CallWithSession(sess, "system", "get_memory_info", nil)
	if err != nil {
		logger.Errorf("Bridge call failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed", "detail": err.Error(), "output": rawResp})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(rawResp), &resp); err != nil {
		logger.Errorf("Invalid bridge response: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response", "detail": err.Error(), "output": rawResp})
		return
	}
	if resp.Status != "ok" {
		logger.Warnf("Bridge returned error: %v", resp.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error, "output": string(resp.Output)})
		return
	}

	var data MemoryResponse
	if err := json.Unmarshal(resp.Output, &data); err != nil {
		logger.Errorf("Invalid output structure: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid output structure", "detail": err.Error(), "output": string(resp.Output)})
		return
	}
	c.JSON(http.StatusOK, data)
}
