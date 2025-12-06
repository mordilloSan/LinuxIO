package system

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/load"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
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

	var resp json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "system", "get_cpu_info", nil, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed", "detail": err.Error()})
		return
	}
	if len(resp) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(http.StatusOK, resp)
}

func handleGetLoad(c *gin.Context) {
	sess := session.SessionFromContext(c)

	var resp json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "system", "get_load_info", nil, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed", "detail": err.Error()})
		return
	}
	if len(resp) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(http.StatusOK, resp)
}
