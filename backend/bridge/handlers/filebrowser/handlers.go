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
		"archive_create":        archiveCreate,
		"archive_extract":       archiveExtract,
		"chmod":                 ipc.WrapSimpleHandler(resourceChmod),
		"users_groups":          ipc.WrapSimpleHandler(usersGroups),
		"file_update_from_temp": fileUpdateFromTemp, // Used by code editor PUT
		// NOTE: file_upload_from_temp, file_download_to_temp, archive_download_setup
		// removed - now using yamux streams (fb-upload, fb-download, fb-archive)
	}
}
