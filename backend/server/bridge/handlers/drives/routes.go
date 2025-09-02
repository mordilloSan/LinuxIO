package drives

import "github.com/gin-gonic/gin"

// RegisterDriveRoutes mounts all /drives endpoints on the given (already-authenticated) group.
func RegisterDriveRoutes(drives *gin.RouterGroup) {
	drives.GET("/info", getDiskInfo)

}
