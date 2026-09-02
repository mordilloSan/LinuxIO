// Package api defines the indexer's HTTP and Server-Sent Events wire contract.
// It deliberately has no dependency on the daemon or storage implementation.
package api

import (
	"strings"
	"time"
	"unicode/utf8"
)

const (
	RouteIndex      = "/index"
	RouteReindex    = "/reindex"
	RouteStatus     = "/status"
	RouteSearch     = "/search"
	RouteDirSize    = "/dirsize"
	RouteSubfolders = "/subfolders"
	RouteAdd        = "/add"
	RouteDelete     = "/delete"
	RouteConfig     = "/config"

	EventStarted  = "started"
	EventProgress = "progress"
	EventComplete = "complete"
	EventError    = "error"

	StatusOK                  = 200
	StatusAccepted            = 202
	StatusBadRequest          = 400
	StatusForbidden           = 403
	StatusConflict            = 409
	StatusInternalServerError = 500

	MinSearchQueryRunes = 3
)

// SearchQueryAllowed applies the public search-length contract after removing
// the existing case-sensitive search modifier.
func SearchQueryAllowed(query string) bool {
	query = strings.ReplaceAll(query, "case:exact", "")
	return utf8.RuneCountInString(strings.TrimSpace(query)) >= MinSearchQueryRunes
}

// IndexerConfig is the persisted daemon configuration exposed by /config.
type IndexerConfig struct {
	ExcludePaths         []string `json:"exclude_paths" yaml:"exclude_paths"`
	IncludeNetworkMounts bool     `json:"include_network_mounts" yaml:"include_network_mounts"`
}

// IndexerConfigPatch is a partial /config update. Pointers preserve explicit false,
// zero, and empty-string values.
type IndexerConfigPatch struct {
	ExcludePaths         *[]string `json:"exclude_paths,omitempty" yaml:"exclude_paths,omitempty"`
	IncludeNetworkMounts *bool     `json:"include_network_mounts,omitempty" yaml:"include_network_mounts,omitempty"`
}

type StatusResponse struct {
	Status            string `json:"status"`
	NumDirs           int64  `json:"num_dirs"`
	NumFiles          int64  `json:"num_files"`
	TotalSize         int64  `json:"total_size"`
	LastIndexed       string `json:"last_indexed"`
	DatabaseSize      int64  `json:"database_size"`
	ActiveOperation   string `json:"active_operation,omitempty"`
	ActiveOperationID string `json:"active_operation_id,omitempty"`
	ActivePath        string `json:"active_path,omitempty"`
	Warning           string `json:"warning,omitempty"`
}

func (resp *StatusResponse) AddWarning(msg string) {
	if resp.Warning == "" {
		resp.Warning = msg
		return
	}
	resp.Warning += "; " + msg
}

type EntryRequest struct {
	Path string `json:"path"`
}

type OperationResponse struct {
	Status      string `json:"status"`
	Path        string `json:"path,omitempty"`
	OperationID string `json:"operation_id,omitempty"`
}

type DirSizeResponse struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Files int64  `json:"files"`
	Dirs  int64  `json:"dirs"`
}

// EntryResult is returned by /search.
type EntryResult struct {
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"mod_time"`
	Inode      uint64    `json:"inode"`
	TotalSize  int64     `json:"total_size,omitempty"`
	TotalFiles int64     `json:"total_files,omitempty"`
	TotalDirs  int64     `json:"total_dirs,omitempty"`
}

type SubfolderResult struct {
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

type Stats struct {
	DatabaseSize int64 `json:"database_size"`
}

type WorkStartedEvent struct {
	Status      string `json:"status"`
	Operation   string `json:"operation"`
	OperationID string `json:"operation_id"`
	Path        string `json:"path"`
}

type WorkProgressEvent struct {
	Operation    string `json:"operation"`
	OperationID  string `json:"operation_id"`
	Path         string `json:"path"`
	Phase        string `json:"phase,omitempty"`
	Message      string `json:"message,omitempty"`
	FilesIndexed int64  `json:"files_indexed,omitempty"`
	DirsIndexed  int64  `json:"dirs_indexed,omitempty"`
	CurrentPath  string `json:"current_path,omitempty"`
	BytesIndexed int64  `json:"bytes_indexed,omitempty"`
}

type WorkCompleteEvent struct {
	Status         string `json:"status"`
	Operation      string `json:"operation"`
	OperationID    string `json:"operation_id"`
	Path           string `json:"path"`
	DurationMs     int64  `json:"duration_ms"`
	FilesIndexed   int64  `json:"files_indexed,omitempty"`
	DirsIndexed    int64  `json:"dirs_indexed,omitempty"`
	TotalSize      int64  `json:"total_size,omitempty"`
	DeletedEntries int64  `json:"deleted_entries,omitempty"`
}

type WorkErrorEvent struct {
	Status      string `json:"status"`
	Operation   string `json:"operation"`
	OperationID string `json:"operation_id"`
	Path        string `json:"path"`
	Message     string `json:"message"`
}
