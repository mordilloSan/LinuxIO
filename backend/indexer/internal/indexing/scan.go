// Portions copyright 2018 File Browser contributors.
// Modified by LinuxIO.
// SPDX-License-Identifier: Apache-2.0

package indexing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// readDirChunk is how many names of a folder are read and looked at before
// the next are asked for, so that a folder with a million files costs a chunk
// of memory and not all of it. A variable for tests.
var readDirChunk = 1024

// indexDirectory indexes the folder a scan starts at, and everything below.
func (idx *Index) indexDirectory(ctx context.Context, adjustedPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Normalize path to always have trailing slash (except for root which is just "/")
	if adjustedPath != "/" {
		adjustedPath = strings.TrimSuffix(adjustedPath, "/") + "/"
	}
	realPath := strings.TrimRight(idx.Path, "/") + adjustedPath
	dir, err := openScanRoot(realPath)
	if err != nil {
		// must have been deleted
		return err
	}
	_, err = idx.indexOpenDirectory(ctx, dir, adjustedPath)
	return err
}

// openScanRoot opens the folder a scan starts at, one folder at a time from
// "/", so that no folder above it is reached through a symlink either. The
// index knows every folder by its real path; a request that names one
// through a link names nothing it holds.
func openScanRoot(path string) (*os.File, error) {
	dir, err := openDirectory(nil, "/", "/")
	if err != nil {
		return nil, err
	}
	for name := range strings.SplitSeq(strings.Trim(path, "/"), "/") {
		if name == "" {
			continue
		}
		next, err := openDirectory(dir, name, path)
		if closeErr := dir.Close(); closeErr != nil {
			slog.Warn("failed to close directory", "path", dir.Name(), "error", closeErr)
		}
		if err != nil {
			return nil, err
		}
		dir = next
	}
	return dir, nil
}

// openDirectory opens the folder name, below parent when there is one, and
// never through a symlink. A scan decides that an entry is a folder and then
// opens it; in a folder another user may write, the entry can be a link by
// then, and root would walk wherever it points.
func openDirectory(parent *os.File, name, path string) (*os.File, error) {
	dirfd := unix.AT_FDCWD
	if parent != nil {
		dirfd = int(parent.Fd())
	}
	fd, err := unix.Openat(dirfd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}

// indexOpenDirectory indexes an open folder and everything below it, sends
// the folder's own entry last, closes the folder and returns its size.
func (idx *Index) indexOpenDirectory(ctx context.Context, dir *os.File, adjustedPath string) (int64, error) {
	defer func() {
		if closeErr := dir.Close(); closeErr != nil {
			slog.Warn("failed to close directory", "path", dir.Name(), "error", closeErr)
		}
	}()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if adjustedPath != "/" {
		adjustedPath = strings.TrimSuffix(adjustedPath, "/") + "/"
	}

	dirInfo, err := dir.Stat()
	if err != nil {
		return 0, err
	}

	// check if excluded from indexing
	if idx.shouldSkip(dirInfo.IsDir(), adjustedPath) {
		return 0, ErrNotIndexed
	}

	entry, err := idx.scanDirectory(ctx, dir, dirInfo, adjustedPath)
	if err != nil {
		return 0, err
	}
	if err := idx.streamWriter.Write(entry); err != nil {
		return 0, fmt.Errorf("%w: directory %s: %v", ErrStreamWrite, entry.RelativePath, err)
	}
	idx.incrementDirCount()
	return entry.Size, nil
}

// scanDirectory indexes every child of an open directory and returns the
// directory's own entry, sized to include everything counted beneath it.
func (idx *Index) scanDirectory(ctx context.Context, dirInfo *os.File, stat os.FileInfo, adjustedPath string) (IndexEntry, error) {
	// Ensure combinedPath has exactly one trailing slash to prevent double slashes in subdirectory paths
	combinedPath := strings.TrimRight(adjustedPath, "/") + "/"

	dirSize := uint64(stat.Size())
	var dirKey devIno
	if allocatedSize, _, key, ok := getFileDetails(stat.Sys()); ok {
		dirSize = allocatedSize
		dirKey = key
	}
	idx.mu.Lock()
	idx.totalSize += dirSize
	idx.mu.Unlock()
	totalSize := int64(dirSize)
	normalizedDir := indexerapi.NormalizePath(adjustedPath)

	for {
		// Names only. ReadDir also wants each entry's type, and on a
		// filesystem that does not report it looks at every entry of the
		// chunk itself: a look that failed would lose the whole folder where
		// indexChild loses one entry, and up to a chunk of slow ones would
		// pass without the walk counting any (Looked).
		names, readErr := dirInfo.Readdirnames(readDirChunk)
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return IndexEntry{}, err
			}
			childSize, err := idx.indexChild(ctx, dirInfo, name, combinedPath, normalizedDir)
			if err != nil {
				return IndexEntry{}, err
			}
			totalSize += childSize
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return IndexEntry{}, readErr
		}
	}

	return IndexEntry{
		RelativePath: normalizedDir,
		Name:         filepath.Base(stat.Name()),
		Size:         totalSize,
		ModTime:      stat.ModTime(),
		Type:         "directory",
		// Both numbers: a move is recognised by the folder at the new path
		// being the one the index holds at the old one.
		Device: dirKey.dev,
		Inode:  dirKey.ino,
	}, nil
}

// indexChild indexes one entry of an open folder and returns what it adds to
// the folder's size. An entry that cannot be looked at or walked into is
// logged and left out; only a failed write or a cancelled scan is an error.
func (idx *Index) indexChild(ctx context.Context, dir *os.File, name, combinedPath, normalizedDir string) (int64, error) {
	fullCombined := combinedPath + name
	file, err := statChild(dir, name)
	// Counted once the look at it has returned, with or without an answer.
	idx.looked.Add(1)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Debug("skipping deleted filesystem entry", "path", fullCombined, "error", err)
		} else {
			slog.Warn("cannot inspect filesystem entry; entry missing from scan", "path", fullCombined, "error", err)
		}
		return 0, nil
	}
	isDir := file.IsDir()
	if idx.shouldSkip(isDir, fullCombined) {
		return 0, nil
	}
	if !isDir {
		return idx.indexChildFile(ctx, file, normalizedDir)
	}
	size, indexErr := idx.indexChildDirectory(ctx, dir, name, fullCombined)
	if indexErr != nil {
		return 0, idx.handleChildDirError(fullCombined, indexErr)
	}
	return size, nil
}

// childInfo is the os.FileInfo of an entry looked at through its folder's
// handle, without following a link.
type childInfo struct {
	name string
	st   unix.Stat_t
}

func (c childInfo) Name() string       { return c.name }
func (c childInfo) Size() int64        { return c.st.Size }
func (c childInfo) ModTime() time.Time { return time.Unix(c.st.Mtim.Sec, c.st.Mtim.Nsec) }
func (c childInfo) IsDir() bool        { return c.st.Mode&unix.S_IFMT == unix.S_IFDIR }

func (c childInfo) Mode() os.FileMode {
	mode := os.FileMode(c.st.Mode & 0o777)
	if c.IsDir() {
		mode |= os.ModeDir
	}
	return mode
}

// Sys gives the part of the stat result the scan reads (getFileDetails).
func (c childInfo) Sys() any {
	return &syscall.Stat_t{Dev: c.st.Dev, Ino: c.st.Ino, Nlink: c.st.Nlink, Mode: c.st.Mode, Size: c.st.Size, Blocks: c.st.Blocks}
}

func statChild(dir *os.File, name string) (os.FileInfo, error) {
	var st unix.Stat_t
	if err := unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, &os.PathError{Op: "lstat", Path: filepath.Join(dir.Name(), name), Err: err}
	}
	return childInfo{name: name, st: st}, nil
}

func (idx *Index) indexChildDirectory(ctx context.Context, parent *os.File, name, dirPath string) (int64, error) {
	realPath := strings.TrimRight(idx.Path, "/") + dirPath
	dir, err := openDirectory(parent, name, realPath)
	if err != nil {
		return 0, err
	}
	return idx.indexOpenDirectory(ctx, dir, dirPath)
}

func (idx *Index) indexChildFile(ctx context.Context, file os.FileInfo, normalizedDir string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	childPath := makeChildRelativePath(normalizedDir, file.Name())
	size, contribution, nlink, key := idx.handleFile(file, childPath)
	idx.incrementFileCount()

	entry := IndexEntry{
		RelativePath:     childPath,
		Name:             file.Name(),
		Size:             int64(size),
		ModTime:          file.ModTime(),
		Type:             "file",
		Device:           key.dev,
		Inode:            key.ino,
		Nlink:            nlink,
		SizeContribution: int64(contribution),
	}
	if err := idx.streamWriter.Write(entry); err != nil {
		return 0, fmt.Errorf("%w: file %s: %v", ErrStreamWrite, childPath, err)
	}
	return int64(contribution), nil
}

// folderDropper is a writer that can be told the scan gave up on a folder,
// so that it removes what it holds of it.
type folderDropper interface {
	Drop(path string) error
}

func (idx *Index) handleChildDirError(dirPath string, err error) error {
	if errors.Is(err, ErrStreamWrite) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, ErrNotIndexed) || errors.Is(err, os.ErrNotExist) {
		slog.Debug("skipping directory", "path", dirPath, "error", err)
	} else {
		idx.incrementSkippedDirs()
		slog.Warn("cannot index directory; subtree missing from scan", "path", dirPath, "error", err)
	}
	if dropper, ok := idx.streamWriter.(folderDropper); ok {
		if dropErr := dropper.Drop(indexerapi.NormalizePath(dirPath)); dropErr != nil {
			return fmt.Errorf("%w: drop directory %s: %v", ErrStreamWrite, dirPath, dropErr)
		}
	}
	return nil
}

func (idx *Index) handleFile(file os.FileInfo, path string) (size, contribution, nlink uint64, key devIno) {
	var realSize uint64
	nlink = 1
	canUseSyscall := false

	if sys := file.Sys(); sys != nil {
		realSize, nlink, key, canUseSyscall = getFileDetails(sys)
	}

	if !canUseSyscall {
		// Fallback for non-unix systems or if syscall info is unavailable
		realSize = uint64(file.Size())
	}

	if nlink > 1 {
		idx.mu.Lock()
		if counted, known := idx.contributors[FileID{Dev: key.dev, Ino: key.ino}]; known {
			if counted != path {
				idx.mu.Unlock()
				return realSize, 0, nlink, key
			}
			idx.processedInodes[key] = struct{}{}
			idx.totalSize += realSize
			idx.mu.Unlock()
			return realSize, realSize, nlink, key
		}
		if _, exists := idx.processedInodes[key]; exists {
			idx.mu.Unlock()
			return realSize, 0, nlink, key
		}
		idx.processedInodes[key] = struct{}{}
		idx.totalSize += realSize
		idx.mu.Unlock()
		return realSize, realSize, nlink, key
	}

	// It's a regular file.
	idx.mu.Lock()
	idx.totalSize += realSize
	idx.mu.Unlock()
	return realSize, realSize, nlink, key
}

func makeChildRelativePath(parent, child string) string {
	if parent == "/" {
		return filepath.Join("/", child)
	}
	parent = strings.TrimSuffix(parent, "/")
	return filepath.Join(parent, child)
}
