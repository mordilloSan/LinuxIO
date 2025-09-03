package api

import "github.com/gin-gonic/gin"

// RegisterSystemRoutes mounts all /system endpoints on the given (already-authenticated) group.
func RegisterSystemRoutes(api *gin.RouterGroup) {
	api.GET("/network", getNetworks)
	api.GET("/info", getHost)
	api.GET("/cpu", getCPU)
	api.GET("/mem", getMem)
	api.GET("/fs", getFileSystem)
	api.GET("/load", getLoadInfo)
	api.GET("/uptime", getUptime)
	api.GET("/processes", getProcesses)
	api.GET("/services", getServices)
	api.GET("/baseboard", getMotherboard)
	api.GET("/gpu", getGPU)
	api.GET("/sensors", getSensors)
	api.GET("/disk", getDisks)
	api.GET("/updates", getUpdates)
}
