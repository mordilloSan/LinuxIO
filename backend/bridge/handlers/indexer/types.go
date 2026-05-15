// Package indexer provides shared utilities for communicating with the
// filesystem indexer service over its unix socket.
package indexer

// IndexerProgress represents progress for indexer operations.
type IndexerProgress struct {
	FilesIndexed int64  `json:"files_indexed"`
	DirsIndexed  int64  `json:"dirs_indexed"`
	CurrentPath  string `json:"current_path,omitempty"`
	Phase        string `json:"phase,omitempty"`
}

// IndexerResult represents the final result of an indexer operation.
type IndexerResult struct {
	Path         string `json:"path"`
	FilesIndexed int64  `json:"files_indexed"`
	DirsIndexed  int64  `json:"dirs_indexed"`
	TotalSize    int64  `json:"total_size"`
	DurationMs   int64  `json:"duration_ms"`
}
