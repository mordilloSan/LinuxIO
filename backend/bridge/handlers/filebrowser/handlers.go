package filebrowser

import (
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func FilebrowserHandlers() map[string]ipc.HandlerFunc {
	return map[string]ipc.HandlerFunc{
		"resource_get":          resourceGet,
		"resource_stat":         resourceStat,
		"resource_delete":       resourceDelete,
		"resource_post":         resourcePost,
		"resource_patch":        resourcePatch,
		"dir_size":              dirSize,
		"subfolders":            subfolders,
		"search":                searchFiles,
		"indexer_status":        indexerStatus,
		"chmod":                 resourceChmod,
		"users_groups":          usersGroups,
		"file_update_from_temp": fileUpdateFromTemp, // Used by code editor PUT
	}
}
