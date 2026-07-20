// Package indexer provides shared utilities for communicating with the
// filesystem indexer service over its unix socket.
package indexer

// IndexerProgress represents progress for indexer operations.
type IndexerProgress struct {
	Status       string `json:"status,omitempty"`
	Operation    string `json:"operation,omitempty"`
	State        string `json:"state,omitempty"`
	Message      string `json:"message,omitempty"`
	Path         string `json:"path,omitempty"`
	FilesIndexed int64  `json:"files_indexed"`
	DirsIndexed  int64  `json:"dirs_indexed"`
	BytesIndexed int64  `json:"bytes_indexed,omitempty"`
	CurrentPath  string `json:"current_path,omitempty"`
	Phase        string `json:"phase,omitempty"`
}

// IndexerResult represents the final result of an indexer operation.
type IndexerResult struct {
	Status         string `json:"status,omitempty"`
	Operation      string `json:"operation,omitempty"`
	Path           string `json:"path"`
	FilesIndexed   int64  `json:"files_indexed"`
	DirsIndexed    int64  `json:"dirs_indexed"`
	TotalSize      int64  `json:"total_size"`
	DurationMs     int64  `json:"duration_ms"`
	DeletedIndexes int    `json:"deleted_indexes,omitempty"`
	DeletedEntries int64  `json:"deleted_entries,omitempty"`
}
