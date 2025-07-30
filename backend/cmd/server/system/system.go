package system

import (
	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/LinuxIO/cmd/server/auth"
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
