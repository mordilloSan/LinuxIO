package filebrowser

import (
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func FilebrowserHandlers() map[string]ipc.HandlerFunc {
	return map[string]ipc.HandlerFunc{
		"resource_get":          ipc.WrapSimpleHandler(resourceGet),
		"resource_stat":         ipc.WrapSimpleHandler(resourceStat),
		"resource_delete":       ipc.WrapSimpleHandler(resourceDelete),
		"resource_post":         ipc.WrapSimpleHandler(resourcePost),
		"resource_patch":        ipc.WrapSimpleHandler(resourcePatch),
		"dir_size":              ipc.WrapSimpleHandler(dirSize),
		"subfolders":            ipc.WrapSimpleHandler(subfolders),
		"search":                ipc.WrapSimpleHandler(searchFiles),
		"indexer_status":        ipc.WrapSimpleHandler(indexerStatus),
		"chmod":                 ipc.WrapSimpleHandler(resourceChmod),
		"users_groups":          ipc.WrapSimpleHandler(usersGroups),
		"file_update_from_temp": ipc.WrapSimpleHandler(fileUpdateFromTemp), // Used by code editor PUT
	}
}
