package system

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/load"

	"github.com/mordilloSan/LinuxIO/common/ipc"
	"github.com/mordilloSan/LinuxIO/common/logger"
	"github.com/mordilloSan/LinuxIO/common/session"
	"github.com/mordilloSan/LinuxIO/server/bridge"
)

type CPUInfoResponse struct {
	VendorID           string             `json:"vendorId"`
	ModelName          string             `json:"modelName"`
	Family             string             `json:"family"`
	Model              string             `json:"model"`
	BaseMHz            float64            `json:"mhz"`
	CurrentFrequencies []float64          `json:"currentFrequencies"`
	Cores              int                `json:"cores"`
	LoadAverage        *load.AvgStat      `json:"loadAverage,omitempty"`
	PerCoreUsage       []float64          `json:"perCoreUsage"`
	Temperature        map[string]float64 `json:"temperature"`
}

type LoadInfoResponse struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

func handleGetCPU(c *gin.Context) {
	sess := session.SessionFromContext(c)
	logger.Infof("%s requested CPU info (session: %s)", sess.User.Username, sess.SessionID)

	rawResp, err := bridge.CallWithSession(sess, "system", "get_cpu_info", nil)
	if err != nil {
		logger.Errorf("Bridge call failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed", "detail": err.Error()})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(rawResp), &resp); err != nil {
		logger.Errorf("Invalid bridge response: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response", "detail": err.Error()})
		return
	}
	if resp.Status != "ok" {
		logger.Warnf("Bridge returned error: %v", resp.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}

	if resp.Output == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(http.StatusOK, resp.Output)
}

func handleGetLoad(c *gin.Context) {
	sess := session.SessionFromContext(c)
	logger.Infof("%s requested load info (session: %s)", sess.User.Username, sess.SessionID)

	rawResp, err := bridge.CallWithSession(sess, "system", "get_load_info", nil)
	if err != nil {
		logger.Errorf("Bridge call failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed", "detail": err.Error()})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(rawResp), &resp); err != nil {
		logger.Errorf("Invalid bridge response: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response", "detail": err.Error()})
		return
	}
	if resp.Status != "ok" {
		logger.Warnf("Bridge returned error: %v", resp.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}

	if resp.Output == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(http.StatusOK, resp.Output)
}
