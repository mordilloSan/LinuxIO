package indexer

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

func indexerStatusToAPI(value Status) apischema.IndexerDaemonStatus {
	result := apischema.IndexerDaemonStatus{
		Running: value.Running, Status: value.Status, NumDirs: int(value.NumDirs), NumFiles: int(value.NumFiles),
		TotalSize: value.TotalSize, FTSActive: value.FTSActive, TotalIndexes: int(value.TotalIndexes),
		TotalEntries: int(value.TotalEntries), DatabaseSize: value.DatabaseSize, WALSize: value.WALSize,
		SHMSize: value.SHMSize, TotalOnDisk: value.TotalOnDisk,
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
