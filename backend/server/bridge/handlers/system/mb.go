package system

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
)

type Baseboard struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Version      string `json:"version"`
	Serial       string `json:"serial"`
}

type BIOS struct {
	Vendor  string `json:"vendor"`
	Version string `json:"version"`
	Date    string `json:"date"`
}

type MotherboardTemperatures struct {
	Socket []float64 `json:"socket"`
}

type MotherboardInfo struct {
	Baseboard    Baseboard               `json:"baseboard"`
	BIOS         BIOS                    `json:"bios"`
	Temperatures MotherboardTemperatures `json:"temperatures"`
}

func handleGetMB(c *gin.Context) {
	sess := session.SessionFromContext(c)

	rawResp, err := bridge.CallWithSession(sess, "system", "get_motherboard_info", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed", "detail": err.Error()})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(rawResp), &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response", "detail": err.Error()})
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

	c.JSON(http.StatusOK, resp.Output)
}
