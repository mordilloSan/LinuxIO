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
	LastIndexed  string `json:"last_indexed,omitempty"`
	DatabaseSize int64  `json:"database_size"`
	ActiveOp     string `json:"active_operation,omitempty"`
	OperationID  string `json:"active_operation_id,omitempty"`
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
		TotalSize: raw.TotalSize, LastIndexed: raw.LastIndexed,
		DatabaseSize: raw.DatabaseSize, ActiveOp: raw.ActiveOperation, OperationID: raw.ActiveOperationID,
		ActivePath: raw.ActivePath, Warning: raw.Warning,
	}
	status.Status = strings.ToLower(strings.TrimSpace(status.Status))
	status.Running = status.Status == "running" || status.Status == "indexing"
	return status, nil
}
