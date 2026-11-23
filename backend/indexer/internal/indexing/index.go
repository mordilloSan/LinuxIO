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
	"sync/atomic"

	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

var (
	// ErrNotIndexed is returned when a path is not indexed.
	ErrNotIndexed = errors.New("path not indexed")
	// ErrStreamWrite marks stream writer failures, which abort the whole
	// traversal rather than skipping the current directory.
	ErrStreamWrite = errors.New("stream writer failed")
)

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

// FileID is devIno for callers outside the package.
type FileID struct {
	Dev uint64
	Ino uint64
}

type Index struct {
	Path                 string // filesystem path being indexed
	NumDirs              uint64
	NumFiles             uint64
	includeNetworkMounts bool                // whether to traverse NFS/SMB/CIFS-style mounts
	excludePaths         []string            // absolute filesystem paths excluded from traversal
	externalMounts       map[string]string   // network mount point -> fs type, snapshotted at Initialize
	overlayMounts        map[string]string   // overlay mount point -> fs type, snapshotted at Initialize
	processedInodes      map[devIno]struct{} // tracks processed (device, inode) pairs for hardlinks
	contributors         map[FileID]string   // the path that carries each known hard link group's size
	totalSize            uint64              // total size
	skippedDirs          uint64              // directories unreadable due to permission/I-O errors
	mu                   sync.RWMutex        // protects concurrent access
	// looked counts the entries the walk has looked at, whatever came of
	// them. It is how a walk that moves is told from one that hangs.
	looked atomic.Uint64

	// Streaming mode fields
	streamWriter StreamingWriter // where to send entries in streaming mode
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

// WithHardlinkContributors names the path that carries the size of each hard
// link group the index already knows. A file in one of those groups counts
// only at that path; groups it does not name count at the first link the
// scan meets.
func WithHardlinkContributors(paths map[FileID]string) Option {
	return func(idx *Index) {
		idx.contributors = paths
	}
}

// Initialize creates a new index for the given path.
// path: the filesystem path to index (e.g., "/", "/home", "/home/user/documents")
func Initialize(path string, opts ...Option) *Index {
	idx := newIndex(path, opts...)
	slog.Debug("initialized index", "path", path, "include_network_mounts", idx.includeNetworkMounts, "exclude_paths", idx.excludePaths)
	return idx
}

func newIndex(path string, opts ...Option) *Index {
	idx := &Index{
		Path:            path,
		processedInodes: make(map[devIno]struct{}),
	}
	for _, opt := range opts {
		opt(idx)
	}
	// Snapshot per check or index run (not per process) so mount changes are
	// visible to later direct mutations and partial reindexes.
	idx.externalMounts = make(map[string]string)
	idx.overlayMounts = make(map[string]string)
	for mountPoint, fsType := range loadMountPointsFn() {
		if fsType == overlayFSType {
			idx.overlayMounts[mountPoint] = fsType
		} else if !idx.includeNetworkMounts {
			idx.externalMounts[mountPoint] = fsType
		}
	}
	return idx
}

// EnableStreaming configures the index to use streaming mode with the provided writer.
// In streaming mode, files are written immediately to the database, and only lightweight
// directory metadata is kept in memory.
func (idx *Index) EnableStreaming(writer StreamingWriter) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.streamWriter = writer
}

// StartIndexing begins indexing the configured path.
func (idx *Index) StartIndexing(ctx context.Context) error {
	idx.mu.RLock()
	writer := idx.streamWriter
	idx.mu.RUnlock()

	if writer == nil {
		return fmt.Errorf("streaming mode is required; call EnableStreaming with a writer before indexing")
	}

	slog.Debug("starting indexing", "path", idx.Path)

	err := idx.indexDirectory(ctx, "/")
	if err != nil {
		return err
	}

	idx.mu.RLock()
	dirs := idx.NumDirs
	files := idx.NumFiles
	skipped := idx.skippedDirs
	idx.mu.RUnlock()
	slog.Debug("completed indexing", "dirs", dirs, "files", files, "skipped_dirs", skipped)
	return nil
}

// StartIndexingFromPath begins indexing from a specific subdirectory within the configured path.
// relativePath should be a normalized path like "/home/user" relative to the index root.
func (idx *Index) StartIndexingFromPath(ctx context.Context, relativePath string) error {
	idx.mu.RLock()
	writer := idx.streamWriter
	idx.mu.RUnlock()

	if writer == nil {
		return fmt.Errorf("streaming mode is required; call EnableStreaming with a writer before indexing")
	}

	slog.Debug("starting partial reindex", "path", relativePath)

	// Normalize the path
	normalizedPath := indexerapi.NormalizePath(relativePath)

	err := idx.indexDirectory(ctx, normalizedPath)
	if err != nil {
		return err
	}

	idx.mu.RLock()
	dirs := idx.NumDirs
	files := idx.NumFiles
	idx.mu.RUnlock()
	slog.Debug("completed partial reindex", "dirs", dirs, "files", files)
	return nil
}

func (idx *Index) incrementDirCount() {
	idx.mu.Lock()
	idx.NumDirs++
	idx.mu.Unlock()
}

func (idx *Index) incrementFileCount() {
	idx.mu.Lock()
	idx.NumFiles++
	idx.mu.Unlock()
}

func (idx *Index) incrementSkippedDirs() {
	idx.mu.Lock()
	idx.skippedDirs++
	idx.mu.Unlock()
}

// Counts returns a consistent snapshot of the live traversal counters, safe
// to call from other goroutines while indexing runs.
func (idx *Index) Counts() (dirs, files, size uint64) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.NumDirs, idx.NumFiles, idx.totalSize
}

// Looked reports how many entries the walk has looked at so far: those it
// indexed, and those it left out, found gone or could not read. NumDirs and
// NumFiles stand still while a walk works through a folder of entries that
// fail one after another; this does not.
func (idx *Index) Looked() uint64 {
	return idx.looked.Load()
}

// SkippedDirCount reports how many directories were skipped because of
// permission or I/O errors (deliberate exclusions are not counted). A
// non-zero value means those subtrees are missing from the scan.
func (idx *Index) SkippedDirCount() uint64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.skippedDirs
}
