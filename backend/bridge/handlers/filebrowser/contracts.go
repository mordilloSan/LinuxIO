package filebrowser

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

type FileCompressResult struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Format string `json:"format"`
}

type FileExtractResult struct {
	Destination string `json:"destination"`
}

type FileBatchResult struct {
	Total     int                    `json:"total"`
	Succeeded int                    `json:"succeeded"`
	Failed    []FileBatchItemFailure `json:"failed"`
}

type FileUploadResult struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type FileUploadBatchResult struct {
	FileBatchResult
	Destination string `json:"destination"`
	Size        int64  `json:"size"`
}

type FileArchiveResult struct {
	ArchiveName string `json:"archiveName"`
	Size        int64  `json:"size"`
	Format      string `json:"format"`
}

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
