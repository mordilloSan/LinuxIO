package system

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

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

	var resp json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "system", "get_motherboard_info", nil, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed", "detail": err.Error()})
		return
	}

	if len(resp) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(http.StatusOK, resp)
}
