package system

import (
	"go-backend/internal/auth"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/host"
)

func RegisterSystemRoutes(router *gin.Engine) {
	system := router.Group("/system", auth.AuthMiddleware())
	{
		system.GET("/info", getHostInfo)
		system.GET("/cpu", getCPUInfo)
		system.GET("/mem", getMemInfo)
		system.GET("/fs", getFsInfo)
		system.GET("/load", getLoadInfo)
		system.GET("/uptime", getUptime)
		system.GET("/processes", getProcesses)
		system.GET("/baseboard", getBaseboardInfo)
		system.GET("/gpu", getGPUInfo)
		system.GET("/sensors", getSensorData)
		system.GET("/disk", getDiskInfo)

	}
}

func getHostInfo(c *gin.Context) {
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
