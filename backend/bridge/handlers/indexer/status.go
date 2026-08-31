package indexer

import (
	"context"
	"strings"
)

// Status is the daemon status shape exposed by the indexer /status endpoint.
type Status struct {
	Running      bool   `json:"running"`
	Status       string `json:"status"`
	NumDirs      int64  `json:"num_dirs"`
	NumFiles     int64  `json:"num_files"`
	TotalSize    int64  `json:"total_size"`
	FTSActive    bool   `json:"fts_active"`
	LastIndexed  string `json:"last_indexed,omitempty"`
	TotalIndexes int64  `json:"total_indexes"`
	TotalEntries int64  `json:"total_entries"`
	DatabaseSize int64  `json:"database_size"`
	WALSize      int64  `json:"wal_size"`
	SHMSize      int64  `json:"shm_size"`
	TotalOnDisk  int64  `json:"total_on_disk"`
	ActiveOp     string `json:"active_operation,omitempty"`
	ActivePath   string `json:"active_path,omitempty"`
	Warning      string `json:"warning,omitempty"`
}

func FetchStatus(ctx context.Context) (Status, error) {
	raw, err := fetchDaemonStatus(ctx)
	if err != nil {
		return Status{}, err
	}
	status := Status{
		Status: raw.Status, NumDirs: raw.NumDirs, NumFiles: raw.NumFiles,
		TotalSize: raw.TotalSize, FTSActive: raw.FTSActive, LastIndexed: raw.LastIndexed,
		TotalIndexes: int64(raw.TotalIndexes), TotalEntries: raw.TotalEntries,
		DatabaseSize: raw.DatabaseSize, WALSize: raw.WALSize, SHMSize: raw.SHMSize,
		TotalOnDisk: raw.TotalOnDisk, ActiveOp: raw.ActiveOperation,
		ActivePath: raw.ActivePath, Warning: raw.Warning,
	}
	status.Status = strings.ToLower(strings.TrimSpace(status.Status))
	status.Running = status.Status == "running" || status.Status == "indexing"
	return status, nil
}
