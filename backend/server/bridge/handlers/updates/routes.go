package updates

import "github.com/gin-gonic/gin"

func RegisterUpdateRoutes(updates *gin.RouterGroup) {
	{
		updates.GET("/packages", getUpdatesHandler)
		updates.POST("/update", updatePackageHandler)
		updates.GET("/update-history", getUpdateHistoryHandler)
		updates.GET("/auto", getAutoUpdatesHandler)
		updates.PUT("/auto", putAutoUpdatesHandler)
		updates.POST("/apply-offline", postApplyOfflineUpdatesHandler)
	}
}
