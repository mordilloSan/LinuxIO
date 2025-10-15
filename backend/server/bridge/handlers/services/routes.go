package services

import "github.com/gin-gonic/gin"

func RegisterServiceRoutes(services *gin.RouterGroup) {
	services.GET("/status", getServiceStatus)
	services.GET("/:name/logs", getServiceLogs)
	services.GET("/:name", getServiceDetail)
	services.POST("/:name/start", startService)
	services.POST("/:name/stop", stopService)
	services.POST("/:name/restart", restartService)
	services.POST("/:name/reload", reloadService)
	services.POST("/:name/enable", enableService)
	services.POST("/:name/disable", disableService)
	services.POST("/:name/mask", maskService)
	services.POST("/:name/unmask", unmaskService)
}
