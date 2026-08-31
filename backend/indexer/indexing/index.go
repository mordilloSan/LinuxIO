// Portions copyright 2018 File Browser contributors.
// Modified by LinuxIO.
// SPDX-License-Identifier: Apache-2.0

package indexing

import (
	"context"
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
	Path                 string // filesystem path being indexed
	NumDirs              uint64
	NumFiles             uint64
	includeNetworkMounts bool                // whether to traverse NFS/SMB/CIFS-style mounts
	excludePaths         []string            // absolute filesystem paths excluded from traversal
	externalMounts       map[string]string   // external mount point -> fs type, snapshotted at Initialize
	processedInodes      map[devIno]struct{} // tracks processed (device, inode) pairs for hardlinks
	totalSize            uint64              // total size
	skippedDirs          uint64              // directories unreadable due to permission/I-O errors
	mu                   sync.RWMutex        // protects concurrent access

	// Streaming mode fields
	streamWriter StreamingWriter        // where to send entries in streaming mode
	dirMetadata  map[string]DirMetadata // lightweight dir metadata in streaming mode
}

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
// path: the filesystem path to index (e.g., "/", "/home", "/home/user/documents")
func Initialize(path string, opts ...Option) *Index {
	newIndex := &Index{
		Path:            path,
		processedInodes: make(map[devIno]struct{}),
	}
	for _, opt := range opts {
		opt(newIndex)
	}
	// Snapshot per index run (not per process) so mounts added while the
	// daemon is running are still excluded from later partial reindexes.
	if !newIndex.includeNetworkMounts {
		newIndex.externalMounts = loadExternalMountPointsFn()
	}
	slog.Info("initialized index", "path", path, "include_network_mounts", newIndex.includeNetworkMounts, "exclude_paths", newIndex.excludePaths)
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
func (idx *Index) StartIndexing(ctx context.Context) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	idx.mu.RLock()
	writer := idx.streamWriter
	idx.mu.RUnlock()

	if writer == nil {
		return fmt.Errorf("streaming mode is required; call EnableStreaming with a writer before indexing")
	}

	slog.Info("starting indexing", "path", idx.Path)

	err := idx.indexDirectory(ctx, "/")
	if err != nil {
		return err
	}

	idx.mu.RLock()
	dirs := idx.NumDirs
	files := idx.NumFiles
	skipped := idx.skippedDirs
	idx.mu.RUnlock()
	slog.Info("completed indexing", "dirs", dirs, "files", files, "skipped_dirs", skipped)
	return nil
}

// StartIndexingFromPath begins indexing from a specific subdirectory within the configured path.
// relativePath should be a normalized path like "/home/user" relative to the index root.
func (idx *Index) StartIndexingFromPath(ctx context.Context, relativePath string) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	idx.mu.RLock()
	writer := idx.streamWriter
	idx.mu.RUnlock()

	if writer == nil {
		return fmt.Errorf("streaming mode is required; call EnableStreaming with a writer before indexing")
	}

	slog.Info("starting partial reindex", "path", relativePath)

	// Normalize the path
	normalizedPath := NormalizeIndexPath(relativePath)

	err := idx.indexDirectory(ctx, normalizedPath)
	if err != nil {
		return err
	}

	idx.mu.RLock()
	dirs := idx.NumDirs
	files := idx.NumFiles
	idx.mu.RUnlock()
	slog.Info("completed partial reindex", "dirs", dirs, "files", files)
	return nil
}

func (idx *Index) GetTotalSize() uint64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.totalSize
}
