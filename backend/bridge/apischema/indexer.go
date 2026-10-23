package apischema

import (
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// IndexerConfig combines daemon-owned configuration with systemd scheduling.
type IndexerConfig struct {
	indexerapi.IndexerConfig
	Interval string `json:"interval"`
}

type IndexerConfigPatch struct {
	indexerapi.IndexerConfigPatch
}

type IndexerConfigSetResult struct {
	Config IndexerConfig `json:"config"`
}

type IndexerTimerSetResult struct {
	Interval string `json:"interval"`
}

type IndexerDaemonStatus struct {
	ActiveOperation   *string `json:"active_operation,omitempty"`
	ActiveOperationID *string `json:"active_operation_id,omitempty"`
	ActivePath        *string `json:"active_path,omitempty"`
	DatabaseSize      int64   `json:"database_size"`
	LastIndexed       *string `json:"last_indexed,omitempty"`
	NumDirs           int     `json:"num_dirs"`
	NumFiles          int     `json:"num_files"`
	Running           bool    `json:"running"`
	Status            string  `json:"status"`
	TotalSize         int64   `json:"total_size"`
	Warning           *string `json:"warning,omitempty"`
}

type IntervalRequest struct {
	Interval string `json:"interval"`
}
