package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/host"
)

func getHost(c *gin.Context) {
	hostInfo, err := host.Info()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get host info", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, hostInfo)
}

func getUptime(c *gin.Context) {
	uptime, err := host.Uptime()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get uptime", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"uptime_seconds": uptime})
}
