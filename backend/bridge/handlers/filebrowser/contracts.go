package filebrowser

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

func indexerStatusToAPI(value indexerStatusResponse) apischema.IndexerStatusResponse {
	result := apischema.IndexerStatusResponse{
		Running: value.Running, Status: value.Status, FTSActive: value.FTSActive,
		FilesIndexed: int(value.FilesIndexed), DirsIndexed: int(value.DirsIndexed), TotalSize: value.TotalSize,
	}
	if value.LastIndexed != "" {
		result.LastIndexed = &value.LastIndexed
	}
	if value.Warning != "" {
		result.Warning = &value.Warning
	}
	return result
}
