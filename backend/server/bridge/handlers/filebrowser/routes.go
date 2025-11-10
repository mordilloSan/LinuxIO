package filebrowser

import (
	"github.com/gin-gonic/gin"
)

// RegisterRoutes wires the Filebrowser HTTP handlers into the provided router group.
// The caller should wrap the group with session middleware before invoking this.
// All handlers are thin HTTP→IPC translators that call bridge handlers.
func RegisterRoutes(r *gin.RouterGroup) error {
	r.GET("/api/resources", resourceGetHandler)
	r.GET("/api/resources/stat", resourceStatHandler)
	r.DELETE("/api/resources", resourceDeleteHandler)
	r.POST("/api/resources", resourcePostHandler)
	r.PUT("/api/resources", resourcePutHandler)
	r.PATCH("/api/resources", resourcePatchHandler)

	r.GET("/api/raw", rawHandler)
	r.GET("/api/dir-size", dirSizeHandler)
	r.GET("/api/multi-stats", multiStatsHandler)

	return nil
}
