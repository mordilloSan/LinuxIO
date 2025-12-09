package filebrowser

import (
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func FilebrowserHandlers() map[string]ipc.HandlerFunc {
	return map[string]ipc.HandlerFunc{
		"resource_get":           ipc.WrapSimpleHandler(resourceGet),
		"resource_stat":          ipc.WrapSimpleHandler(resourceStat),
		"resource_delete":        ipc.WrapSimpleHandler(resourceDelete),
		"resource_post":          ipc.WrapSimpleHandler(resourcePost),
		"resource_put":           ipc.WrapSimpleHandler(resourcePut),
		"resource_patch":         ipc.WrapSimpleHandler(resourcePatch),
		"raw_files":              ipc.WrapSimpleHandler(rawFiles),
		"dir_size":               ipc.WrapSimpleHandler(dirSize),
		"archive_create":         archiveCreate,
		"archive_extract":        ipc.WrapSimpleHandler(archiveExtract),
		"chmod":                  ipc.WrapSimpleHandler(resourceChmod),
		"users_groups":           ipc.WrapSimpleHandler(usersGroups),
		"file_upload_stream":     fileUploadFromTemp,
		"file_download_to_temp":  fileDownloadToTemp,
		"archive_download_setup": archiveDownloadSetup,
	}
}
