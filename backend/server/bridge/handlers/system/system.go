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
}
