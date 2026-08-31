package indexing

import (
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing/iteminfo"
)

// IndexEntry captures a flattened representation of either a directory or a file.
type IndexEntry struct {
	RelativePath string
	AbsolutePath string
	Name         string
	Size         int64
	ModTime      time.Time
	Type         string // "directory" or "file"
	Hidden       bool
	Inode        uint64
}

// NormalizeIndexPath returns the canonical relative path representation used throughout the index:
// always leading "/", no trailing "/" (except for root which stays "/").
// It also cleans the path to prevent path traversal attacks (e.g., "/../../../etc").
func NormalizeIndexPath(p string) string {
	if p == "" || p == "/" {
		return "/"
	}
	// Clean the path to resolve ".." and "." sequences, preventing path traversal
	cleaned := filepath.Clean("/" + strings.Trim(p, "/"))
	if cleaned == "" || cleaned == "." {
		return "/"
	}
	return cleaned
}

// ValidateRelativePath checks if a path is safe (doesn't escape the root via traversal).
// Only ".." path segments are rejected; names that merely contain consecutive
// dots (e.g. "notes..txt") are valid.
func ValidateRelativePath(p string) bool {
	return !slices.Contains(strings.Split(p, "/"), "..")
}

// BoolToInt returns 1 for true, 0 for false.
func BoolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func makeChildRelativePath(parent, child string) string {
	if parent == "/" {
		return filepath.Join("/", child)
	}
	parent = strings.TrimSuffix(parent, "/")
	return filepath.Join(parent, child)
}

func directoryName(info *iteminfo.FileInfo, fallbackPath string) string {
	if info != nil && info.Name != "" {
		return info.Name
	}
	trimmed := strings.Trim(fallbackPath, "/")
	if trimmed == "" {
		return "/"
	}
	parts := strings.Split(trimmed, "/")
	return parts[len(parts)-1]
}
