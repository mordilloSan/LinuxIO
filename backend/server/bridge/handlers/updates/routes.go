package updates

import "github.com/gin-gonic/gin"

// RegisterUpdateRoutes mounts update-related HTTP endpoints.
// Note: Most update operations have been migrated to stream API (yamux).
// Only /update-history remains as it reads server-side log files directly.
func RegisterUpdateRoutes(updates *gin.RouterGroup) {
	updates.GET("/update-history", getUpdateHistoryHandler)
}
