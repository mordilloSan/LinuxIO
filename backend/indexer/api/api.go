// Package api defines the indexer's HTTP and Server-Sent Events wire contract.
// It deliberately has no dependency on the daemon or storage implementation.
package api

import "time"

const (
	ProtocolVersion = 1

	RouteOpenAPI    = "/openapi.json"
	RouteIndex      = "/index"
	RouteReindex    = "/reindex"
	RouteVacuum     = "/vacuum"
	RoutePrune      = "/prune"
	RouteStatus     = "/status"
	RouteSearch     = "/search"
	RouteDirSize    = "/dirsize"
	RouteEntryCount = "/entrycount"
	RouteSubfolders = "/subfolders"
	RouteEntries    = "/entries"
	RouteAdd        = "/add"
	RouteDelete     = "/delete"
	RouteConfig     = "/config"

	RestartRequiredHeader = "X-Indexer-Restart-Required"

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
)

type IndexerIntegrityCheck string

// IndexerConfig is the persisted daemon configuration exposed by /config.
type IndexerConfig struct {
	IndexPath            string                `json:"index_path" yaml:"index_path"`
	IndexName            string                `json:"index_name" yaml:"index_name"`
	ExcludePaths         []string              `json:"exclude_paths" yaml:"exclude_paths"`
	IncludeHidden        bool                  `json:"include_hidden" yaml:"include_hidden"`
	IncludeNetworkMounts bool                  `json:"include_network_mounts" yaml:"include_network_mounts"`
	FreshIndex           bool                  `json:"fresh_index" yaml:"fresh_index"`
	FTSSearch            bool                  `json:"fts_search" yaml:"fts_search"`
	KeepIndexes          int                   `json:"keep_indexes" yaml:"keep_indexes"`
	IntegrityCheck       IndexerIntegrityCheck `json:"integrity_check" yaml:"integrity_check"`
	DBPath               string                `json:"db_path" yaml:"db_path"`
	DBBusyTimeout        string                `json:"db_busy_timeout" yaml:"db_busy_timeout"`
	DBJournalMode        string                `json:"db_journal_mode" yaml:"db_journal_mode"`
	DBSynchronous        string                `json:"db_synchronous" yaml:"db_synchronous"`
	DBAutoVacuum         string                `json:"db_auto_vacuum" yaml:"db_auto_vacuum"`
	DBMaxOpenConns       int                   `json:"db_max_open_conns" yaml:"db_max_open_conns"`
	DBMaxIdleConns       int                   `json:"db_max_idle_conns" yaml:"db_max_idle_conns"`
	DBConnMaxIdleTime    string                `json:"db_conn_max_idle_time" yaml:"db_conn_max_idle_time"`
	DBStmtCacheSize      int                   `json:"db_stmt_cache_size" yaml:"db_stmt_cache_size"`
	SearchDefaultLimit   int                   `json:"search_default_limit" yaml:"search_default_limit"`
	SearchMaxLimit       int                   `json:"search_max_limit" yaml:"search_max_limit"`
	EntriesDefaultLimit  int                   `json:"entries_default_limit" yaml:"entries_default_limit"`
	EntriesMaxLimit      int                   `json:"entries_max_limit" yaml:"entries_max_limit"`
	SocketPath           string                `json:"socket_path" yaml:"socket_path"`
	ListenAddr           string                `json:"listen_addr" yaml:"listen_addr"`
	Interval             string                `json:"interval" yaml:"interval"`
	IdleTimeout          string                `json:"idle_timeout" yaml:"idle_timeout"`
}

// Config preserves the indexer's internal name for its canonical wire type.
type Config = IndexerConfig

// IndexerConfigPatch is a partial /config update. Pointers preserve explicit false,
// zero, and empty-string values.
type IndexerConfigPatch struct {
	IndexPath            *string                `json:"index_path,omitempty" yaml:"index_path,omitempty"`
	IndexName            *string                `json:"index_name,omitempty" yaml:"index_name,omitempty"`
	ExcludePaths         *[]string              `json:"exclude_paths,omitempty" yaml:"exclude_paths,omitempty"`
	IncludeHidden        *bool                  `json:"include_hidden,omitempty" yaml:"include_hidden,omitempty"`
	IncludeNetworkMounts *bool                  `json:"include_network_mounts,omitempty" yaml:"include_network_mounts,omitempty"`
	FreshIndex           *bool                  `json:"fresh_index,omitempty" yaml:"fresh_index,omitempty"`
	FTSSearch            *bool                  `json:"fts_search,omitempty" yaml:"fts_search,omitempty"`
	KeepIndexes          *int                   `json:"keep_indexes,omitempty" yaml:"keep_indexes,omitempty"`
	IntegrityCheck       *IndexerIntegrityCheck `json:"integrity_check,omitempty" yaml:"integrity_check,omitempty"`
	DBPath               *string                `json:"db_path,omitempty" yaml:"db_path,omitempty"`
	DBBusyTimeout        *string                `json:"db_busy_timeout,omitempty" yaml:"db_busy_timeout,omitempty"`
	DBJournalMode        *string                `json:"db_journal_mode,omitempty" yaml:"db_journal_mode,omitempty"`
	DBSynchronous        *string                `json:"db_synchronous,omitempty" yaml:"db_synchronous,omitempty"`
	DBAutoVacuum         *string                `json:"db_auto_vacuum,omitempty" yaml:"db_auto_vacuum,omitempty"`
	DBMaxOpenConns       *int                   `json:"db_max_open_conns,omitempty" yaml:"db_max_open_conns,omitempty"`
	DBMaxIdleConns       *int                   `json:"db_max_idle_conns,omitempty" yaml:"db_max_idle_conns,omitempty"`
	DBConnMaxIdleTime    *string                `json:"db_conn_max_idle_time,omitempty" yaml:"db_conn_max_idle_time,omitempty"`
	DBStmtCacheSize      *int                   `json:"db_stmt_cache_size,omitempty" yaml:"db_stmt_cache_size,omitempty"`
	SearchDefaultLimit   *int                   `json:"search_default_limit,omitempty" yaml:"search_default_limit,omitempty"`
	SearchMaxLimit       *int                   `json:"search_max_limit,omitempty" yaml:"search_max_limit,omitempty"`
	EntriesDefaultLimit  *int                   `json:"entries_default_limit,omitempty" yaml:"entries_default_limit,omitempty"`
	EntriesMaxLimit      *int                   `json:"entries_max_limit,omitempty" yaml:"entries_max_limit,omitempty"`
	SocketPath           *string                `json:"socket_path,omitempty" yaml:"socket_path,omitempty"`
	ListenAddr           *string                `json:"listen_addr,omitempty" yaml:"listen_addr,omitempty"`
	Interval             *string                `json:"interval,omitempty" yaml:"interval,omitempty"`
	IdleTimeout          *string                `json:"idle_timeout,omitempty" yaml:"idle_timeout,omitempty"`
}

// ConfigPatch preserves the indexer's internal name for its canonical wire type.
type ConfigPatch = IndexerConfigPatch

type StatusResponse struct {
	ProtocolVersion     int    `json:"protocol_version"`
	Status              string `json:"status"`
	NumDirs             int64  `json:"num_dirs"`
	NumFiles            int64  `json:"num_files"`
	TotalSize           int64  `json:"total_size"`
	FTSActive           bool   `json:"fts_active"`
	LastIndexed         string `json:"last_indexed"`
	TotalIndexes        int    `json:"total_indexes"`
	TotalEntries        int64  `json:"total_entries"`
	DatabaseSize        int64  `json:"database_size"`
	WALSize             int64  `json:"wal_size"`
	SHMSize             int64  `json:"shm_size"`
	TotalOnDisk         int64  `json:"total_on_disk"`
	RSSBytes            int64  `json:"rss_bytes"`
	GoAllocBytes        uint64 `json:"go_alloc_bytes"`
	GoHeapInuseBytes    uint64 `json:"go_heap_inuse_bytes"`
	GoHeapIdleBytes     uint64 `json:"go_heap_idle_bytes"`
	GoHeapReleasedBytes uint64 `json:"go_heap_released_bytes"`
	GoSysBytes          uint64 `json:"go_sys_bytes"`
	GoNumGC             uint32 `json:"go_num_gc"`
	CgroupCurrent       int64  `json:"cgroup_memory_current_bytes"`
	CgroupAnon          int64  `json:"cgroup_memory_anon_bytes"`
	CgroupFile          int64  `json:"cgroup_memory_file_bytes"`
	ActiveOperation     string `json:"active_operation,omitempty"`
	ActivePath          string `json:"active_path,omitempty"`
	Warning             string `json:"warning,omitempty"`
}

func (resp *StatusResponse) AddWarning(msg string) {
	if resp.Warning == "" {
		resp.Warning = msg
		return
	}
	resp.Warning += "; " + msg
}

type EntryRequest struct {
	Path    string `json:"path"`
	AbsPath string `json:"absPath"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Type    string `json:"type"`
	Hidden  bool   `json:"hidden"`
	ModUnix int64  `json:"modUnix"`
	Inode   uint64 `json:"inode"`
}

type OperationResponse struct {
	Status     string `json:"status"`
	Path       string `json:"path,omitempty"`
	KeepLatest string `json:"keep_latest,omitempty"`
	MaxAgeDays string `json:"max_age_days,omitempty"`
}

type DirSizeResponse struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type EntryCountResponse struct {
	Path  string `json:"path"`
	Files int64  `json:"files"`
	Dirs  int64  `json:"dirs"`
}

// EntryResult is returned by /search and /entries.
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
	TotalIndexes int       `json:"total_indexes"`
	TotalEntries int64     `json:"total_entries"`
	TotalSize    int64     `json:"total_size"`
	LastScanTime time.Time `json:"last_scan_time"`
	DatabaseSize int64     `json:"database_size"`
	WALSize      int64     `json:"wal_size"`
	SHMSize      int64     `json:"shm_size"`
	TotalOnDisk  int64     `json:"total_on_disk"`
}

type WorkStartedEvent struct {
	Status    string `json:"status"`
	Operation string `json:"operation"`
	Path      string `json:"path,omitempty"`
}

type WorkProgressEvent struct {
	Operation    string `json:"operation"`
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
	Path           string `json:"path,omitempty"`
	DurationMs     int64  `json:"duration_ms"`
	FilesIndexed   int64  `json:"files_indexed,omitempty"`
	DirsIndexed    int64  `json:"dirs_indexed,omitempty"`
	TotalSize      int64  `json:"total_size,omitempty"`
	DeletedIndexes int    `json:"deleted_indexes,omitempty"`
	DeletedEntries int64  `json:"deleted_entries,omitempty"`
}
