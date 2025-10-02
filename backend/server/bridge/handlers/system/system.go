package system

import (
	"github.com/gin-gonic/gin"
)

func RegisterSystemRoutes(sys *gin.RouterGroup) {
	sys.GET("/cpu", handleGetCPU)
	sys.GET("/load", handleGetLoad)
	sys.GET("/sensors", handleGetSensors)
	sys.GET("/baseboard", handleGetMB)
	sys.GET("/memory", handleGetMemory)
	sys.GET("/mem", handleGetMemory)
	sys.GET("/info", handleGetInfo)
	sys.GET("/fs", handleGetFS)
	sys.GET("/uptime", handleGetUptime)
	sys.GET("/processes", handleGetProcesses)
	sys.GET("/services", handleGetServices)
	sys.GET("/gpu", handleGetGPU)
	sys.GET("/disk", handleGetDisk)
	sys.GET("/updates", handleGetFastUpdates)
	sys.GET("/network", handleGetNetwork)

}
