// Portions copyright 2018 File Browser contributors.
// Modified by LinuxIO.
// SPDX-License-Identifier: Apache-2.0

package indexing

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

var (
	// ErrNotIndexed is returned when a path is not indexed.
	ErrNotIndexed = errors.New("path not indexed")
	// ErrStreamWrite marks stream writer failures, which abort the whole
	// traversal rather than skipping the current directory.
	ErrStreamWrite = errors.New("stream writer failed")
)

// ReducedIndex is the JSON-friendly representation exposed to clients.
type ReducedIndex struct {
	IdxName         string      `json:"name"`
	DiskUsed        uint64      `json:"used"`
	DiskTotal       uint64      `json:"total"`
	Status          IndexStatus `json:"status"`
	NumDirs         uint64      `json:"numDirs"`
	NumFiles        uint64      `json:"numFiles"`
	LastIndexed     time.Time   `json:"-"`
	LastIndexedUnix int64       `json:"lastIndexedUnixTime"`
}

// DirMetadata stores minimal per-directory metadata; the map key is the path to avoid duplicating it.
type DirMetadata struct {
	Size    int64
	ModTime time.Time
	Inode   uint64
}

// StreamingWriter is an interface for writing entries as they are discovered.
type StreamingWriter interface {
	Write(entry IndexEntry) error
}

// devIno identifies a file across all scanned filesystems. Inode numbers are
// only unique within one device, so hardlink deduplication must key on both —
// keying on inode alone conflates unrelated files on different mounts.
type devIno struct {
	dev uint64
	ino uint64
}

type Index struct {
	ReducedIndex
	Name                 string              // unique name for this index
	Path                 string              // filesystem path being indexed
	Source               string              // source identifier
	includeHidden        bool                // whether to include hidden files and directories
	includeNetworkMounts bool                // whether to traverse NFS/SMB/CIFS-style mounts
	excludePaths         []string            // absolute filesystem paths excluded from traversal
	externalMounts       map[string]string   // external mount point -> fs type, snapshotted at Initialize
	FoundHardLinks       map[string]uint64   `json:"-"` // hardlink path -> size
	processedInodes      map[devIno]struct{} `json:"-"` // tracks processed (device, inode) pairs for hardlinks
	totalSize            uint64              `json:"-"` // total size
	skippedDirs          uint64              `json:"-"` // directories unreadable due to permission/I-O errors
	mu                   sync.RWMutex        `json:"-"` // protects concurrent access

	// Streaming mode fields
	streamWriter StreamingWriter        `json:"-"` // where to send entries in streaming mode
	dirMetadata  map[string]DirMetadata `json:"-"` // lightweight dir metadata in streaming mode
}

type IndexStatus string

const (
	READY       IndexStatus = "ready"
	INDEXING    IndexStatus = "indexing"
	UNAVAILABLE IndexStatus = "unavailable"
)

type Option func(*Index)

// WithNetworkMounts allows traversal into network/external mount points.
func WithNetworkMounts(include bool) Option {
	return func(idx *Index) {
		idx.includeNetworkMounts = include
	}
}

// WithExcludePaths prevents traversal into the configured absolute paths.
func WithExcludePaths(paths []string) Option {
	return func(idx *Index) {
		idx.excludePaths = append([]string(nil), paths...)
	}
}

// Initialize creates a new index for the given path.
// name: a unique name for this index
// path: the filesystem path to index (e.g., "/", "/home", "/home/user/documents")
// source: an optional source identifier (can be same as name)
// includeHidden: whether to include hidden files and directories (starting with .)
func Initialize(name string, path string, source string, includeHidden bool, opts ...Option) *Index {
	newIndex := &Index{
		Name:            name,
		Path:            path,
		Source:          source,
		includeHidden:   includeHidden,
		processedInodes: make(map[devIno]struct{}),
		FoundHardLinks:  make(map[string]uint64),
	}
	for _, opt := range opts {
		opt(newIndex)
	}
	// Snapshot per index run (not per process) so mounts added while the
	// daemon is running are still excluded from later partial reindexes.
	if !newIndex.includeNetworkMounts {
		newIndex.externalMounts = loadExternalMountPointsFn()
	}
	newIndex.ReducedIndex = ReducedIndex{
		Status:  READY,
		IdxName: name,
	}

	slog.Info("initialized index", "name", name, "path", path, "include_hidden", includeHidden, "include_network_mounts", newIndex.includeNetworkMounts, "exclude_paths", newIndex.excludePaths)
	return newIndex
}

// EnableStreaming configures the index to use streaming mode with the provided writer.
// In streaming mode, files are written immediately to the database, and only lightweight
// directory metadata is kept in memory.
func (idx *Index) EnableStreaming(writer StreamingWriter) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.streamWriter = writer
	idx.dirMetadata = make(map[string]DirMetadata)
}

// StartIndexing begins indexing the configured path.
func (idx *Index) StartIndexing() error {
	idx.mu.RLock()
	writer := idx.streamWriter
	idx.mu.RUnlock()

	if writer == nil {
		return fmt.Errorf("streaming mode is required; call EnableStreaming with a writer before indexing")
	}

	idx.SetStatus(INDEXING)
	slog.Info("starting indexing", "name", idx.Name, "path", idx.Path)

	err := idx.indexDirectory("/")
	if err != nil {
		idx.SetStatus(UNAVAILABLE)
		return err
	}

	idx.SetStatus(READY)
	idx.mu.RLock()
	dirs := idx.NumDirs
	files := idx.NumFiles
	skipped := idx.skippedDirs
	idx.mu.RUnlock()
	slog.Info("completed indexing", "name", idx.Name, "dirs", dirs, "files", files, "skipped_dirs", skipped)
	return nil
}

// StartIndexingFromPath begins indexing from a specific subdirectory within the configured path.
// relativePath should be a normalized path like "/home/user" relative to the index root.
func (idx *Index) StartIndexingFromPath(relativePath string) error {
	idx.mu.RLock()
	writer := idx.streamWriter
	idx.mu.RUnlock()

	if writer == nil {
		return fmt.Errorf("streaming mode is required; call EnableStreaming with a writer before indexing")
	}

	slog.Info("starting partial reindex", "name", idx.Name, "path", relativePath)

	// Normalize the path
	normalizedPath := NormalizeIndexPath(relativePath)

	err := idx.indexDirectory(normalizedPath)
	if err != nil {
		return err
	}

	idx.mu.RLock()
	dirs := idx.NumDirs
	files := idx.NumFiles
	idx.mu.RUnlock()
	slog.Info("completed partial reindex", "name", idx.Name, "dirs", dirs, "files", files)
	return nil
}

func (idx *Index) SetStatus(status IndexStatus) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.Status = status
}

func (idx *Index) GetTotalSize() uint64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.totalSize
}
