package system

import "github.com/gin-gonic/gin"

// RegisterSystemRoutes mounts all /system endpoints on the given (already-authenticated) group.
func RegisterSystemRoutes(system *gin.RouterGroup) {
	system.GET("/network", getNetworks)
	system.GET("/info", getHost)
	system.GET("/cpu", getCPU)
	system.GET("/mem", getMem)
	system.GET("/fs", getFileSystem)
	system.GET("/load", getLoadInfo)
	system.GET("/uptime", getUptime)
	system.GET("/processes", getProcesses)
	system.GET("/services", getServices)
	system.GET("/baseboard", getMotherboard)
	system.GET("/gpu", getGPU)
	system.GET("/sensors", getSensors)
	system.GET("/disk", getDisks)
	system.GET("/updates", getUpdates)
}
