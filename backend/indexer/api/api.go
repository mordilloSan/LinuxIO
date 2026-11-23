// Package api defines the indexer's HTTP and Server-Sent Events wire contract.
// It deliberately has no dependency on the daemon or storage implementation.
package api

import (
	"encoding/json/jsontext"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	RouteIndex      = "/index"
	RouteReindex    = "/reindex"
	RouteStatus     = "/status"
	RouteOperation  = "/operation"
	RouteSearch     = "/search"
	RouteDirSize    = "/dirsize"
	RouteSubfolders = "/subfolders"
	RouteAdd        = "/add"
	RouteDelete     = "/delete"
	RouteRefresh    = "/refresh"
	RouteMove       = "/move"
	RouteScan       = "/scan"
	RouteScanEvent  = "/scan/event"
	RouteConfig     = "/config"

	EventStarted  = "started"
	EventProgress = "progress"
	EventComplete = "complete"
	EventError    = "error"

	// Words the daemon puts in a "status" field. /status reports
	// uninitialized (no index yet), running (work in progress) or idle; an
	// accepted operation answers running, and its work events running,
	// complete or error; /add and /delete answer ok when the update was applied
	// and queued when it waits behind other work.
	StatusUninitialized = "uninitialized"
	StatusIdle          = "idle"
	StatusRunning       = "running"
	StatusComplete      = "complete"
	StatusError         = "error"
	StatusOK            = "ok"
	StatusQueued        = "queued"

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
	ExcludePaths         *[]string `json:"exclude_paths,omitzero" yaml:"exclude_paths,omitempty"`
	IncludeNetworkMounts *bool     `json:"include_network_mounts,omitzero" yaml:"include_network_mounts,omitempty"`
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
	// WaitingOperations are the index and reindex operations queued behind the
	// one that runs, in the order they will run.
	WaitingOperations []WaitingOperation `json:"waiting_operations,omitempty"`
}

// WaitingOperation names an operation that waits for its turn and can be
// attached to by these three values.
type WaitingOperation struct {
	Operation   string `json:"operation"`
	OperationID string `json:"operation_id"`
	Path        string `json:"path"`
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

// MoveRequest names a path that a file operation moved, and where it is now.
type MoveRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
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
	// Pending is set while work that can change what is counted below the
	// folder runs or waits: the numbers are those from before it.
	Pending bool `json:"pending,omitempty"`
}

// EntryResult is returned by /search.
type EntryResult struct {
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"mod_time"`
	Inode      uint64    `json:"inode"`
	TotalSize  int64     `json:"total_size,omitzero"`
	TotalFiles int64     `json:"total_files,omitzero"`
	TotalDirs  int64     `json:"total_dirs,omitzero"`
}

// SubfoldersResponse is returned by /subfolders.
type SubfoldersResponse struct {
	Subfolders []SubfolderResult `json:"subfolders"`
	// Pending is set as in DirSizeResponse, for the folder asked about.
	Pending bool `json:"pending,omitempty"`
}

type SubfolderResult struct {
	Path    string    `json:"path"`
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
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
	FilesIndexed int64  `json:"files_indexed,omitzero"`
	DirsIndexed  int64  `json:"dirs_indexed,omitzero"`
	CurrentPath  string `json:"current_path,omitempty"`
	BytesIndexed int64  `json:"bytes_indexed,omitzero"`
}

type WorkCompleteEvent struct {
	Status         string `json:"status"`
	Operation      string `json:"operation"`
	OperationID    string `json:"operation_id"`
	Path           string `json:"path"`
	DurationMs     int64  `json:"duration_ms"`
	FilesIndexed   int64  `json:"files_indexed,omitzero"`
	DirsIndexed    int64  `json:"dirs_indexed,omitzero"`
	TotalSize      int64  `json:"total_size,omitzero"`
	DeletedEntries int64  `json:"deleted_entries,omitzero"`
}

type WorkErrorEvent struct {
	Status      string `json:"status"`
	Operation   string `json:"operation"`
	OperationID string `json:"operation_id"`
	Path        string `json:"path"`
	Message     string `json:"message"`
}

// OperationOutcome is the last event of an index or reindex operation that is
// over: EventComplete or EventError, with that event's data. The daemon holds
// the outcomes of the operations it ended lately, in memory, so one asked of a
// daemon that started since is not found.
type OperationOutcome struct {
	Event string         `json:"event"`
	Data  jsontext.Value `json:"data"`
}

// NormalizePath is the canonical index path both the daemon and the bridge
// use: cleaned (so ".." cannot escape the root), absolute, no trailing slash,
// and "/" for the root.
func NormalizePath(p string) string {
	if p == "" || p == "/" {
		return "/"
	}
	cleaned := filepath.Clean("/" + strings.Trim(p, "/"))
	if cleaned == "" || cleaned == "." {
		return "/"
	}
	return cleaned
}
