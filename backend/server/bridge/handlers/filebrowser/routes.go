package filebrowser

import (
	"github.com/gin-gonic/gin"
)

// RegisterRoutes wires the Filebrowser HTTP handlers into the provided router group.
// The caller should wrap the group with session middleware before invoking this.
// All handlers are thin HTTP→IPC translators that call bridge handlers.
func RegisterRoutes(r *gin.RouterGroup) error {
	// Resource CRUD → bridge commands: resource_get, resource_stat, resource_delete,
	// resource_post (dirs/uploads), file_update_from_temp, resource_patch.
	r.GET("/api/resources", resourceGetHandler)       // metadata (bridge: resource_get)
	r.GET("/api/resources/stat", resourceStatHandler) // stat/details (bridge: resource_stat)
	r.DELETE("/api/resources", resourceDeleteHandler) // delete (bridge: resource_delete)
	r.POST("/api/resources", resourcePostHandler)     // create dir only (bridge: resource_post) - uploads via yamux streams
	r.PUT("/api/resources", resourcePutHandler)       // overwrite file (bridge: file_update_from_temp)
	r.PATCH("/api/resources", resourcePatchHandler)   // move/copy/rename (bridge: resource_patch)

	// Transfer/metadata helpers.
	// NOTE: /api/raw removed - downloads now use yamux streams (fb-download, fb-archive)
	r.GET("/api/dir-size", dirSizeHandler)                  // single dir size (bridge: dir_size)
	r.GET("/api/subfolders", subfoldersHandler)             // batch folder sizes (bridge: subfolders)
	r.GET("/api/search", searchHandler)                     // search files (bridge: search)
	r.POST("/api/archive/compress", archiveCompressHandler) // compress (bridge: archive_create)
	r.POST("/api/archive/extract", archiveExtractHandler)   // extract (bridge: archive_extract)
	r.POST("/api/chmod", chmodHandler)                      // perms/ownership (bridge: chmod)
	r.GET("/api/users-groups", usersGroupsHandler)          // system users/groups (bridge: users_groups)

	return nil
}
