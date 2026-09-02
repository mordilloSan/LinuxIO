package indexer

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

func indexerStatusToAPI(value Status) apischema.IndexerDaemonStatus {
	result := apischema.IndexerDaemonStatus{
		Running: value.Running, Status: value.Status, NumDirs: int(value.NumDirs), NumFiles: int(value.NumFiles),
		TotalSize: value.TotalSize, DatabaseSize: value.DatabaseSize,
	}
	if value.OperationID != "" {
		result.ActiveOperationID = &value.OperationID
	}
	if value.LastIndexed != "" {
		result.LastIndexed = &value.LastIndexed
	}
	if value.ActiveOp != "" {
		result.ActiveOperation = &value.ActiveOp
	}
	if value.ActivePath != "" {
		result.ActivePath = &value.ActivePath
	}
	if value.Warning != "" {
		result.Warning = &value.Warning
	}
	return result
}
