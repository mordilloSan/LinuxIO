// Portions copyright 2018 File Browser contributors.
// Modified by LinuxIO.
// SPDX-License-Identifier: Apache-2.0

package indexing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// indexDirectory recursively indexes files and directories.
func (idx *Index) indexDirectory(ctx context.Context, adjustedPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Normalize path to always have trailing slash (except for root which is just "/")
	if adjustedPath != "/" {
		adjustedPath = strings.TrimSuffix(adjustedPath, "/") + "/"
	}
	realPath := strings.TrimRight(idx.Path, "/") + adjustedPath
	// Open the directory
	dir, err := os.Open(realPath)
	if err != nil {
		// must have been deleted
		return err
	}
	defer func() {
		if closeErr := dir.Close(); closeErr != nil {
			slog.Warn("failed to close directory", "path", realPath, "err", closeErr)
		}
	}()

	dirInfo, err := dir.Stat()
	if err != nil {
		return err
	}

	// check if excluded from indexing
	if idx.shouldSkip(dirInfo.IsDir(), adjustedPath) {
		return ErrNotIndexed
	}

	entry, err := idx.scanDirectory(ctx, dir, dirInfo, adjustedPath)
	if err != nil {
		return err
	}

	idx.mu.Lock()
	idx.dirMetadata[entry.RelativePath] = DirMetadata{
		Size:    entry.Size,
		ModTime: entry.ModTime,
		Inode:   entry.Inode,
	}
	idx.mu.Unlock()

	if err := idx.streamWriter.Write(entry); err != nil {
		return fmt.Errorf("%w: directory %s: %v", ErrStreamWrite, entry.RelativePath, err)
	}
	idx.incrementDirCount()
	return nil
}

// scanDirectory indexes every child of an open directory and returns the
// directory's own entry, sized to include everything counted beneath it.
func (idx *Index) scanDirectory(ctx context.Context, dirInfo *os.File, stat os.FileInfo, adjustedPath string) (IndexEntry, error) {
	// Ensure combinedPath has exactly one trailing slash to prevent double slashes in subdirectory paths
	combinedPath := strings.TrimRight(adjustedPath, "/") + "/"
	// Read directory contents
	files, err := readableDirEntries(dirInfo, combinedPath)
	if err != nil {
		return IndexEntry{}, err
	}

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
	dirHidden := isHidden(stat)
	normalizedDir := indexerapi.NormalizePath(adjustedPath)

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return IndexEntry{}, err
		}
		hidden := isHidden(file)
		isDir := file.IsDir()
		baseName := file.Name()
		fullCombined := combinedPath + baseName
		if idx.shouldSkip(isDir, fullCombined) {
			continue
		}

		if isDir {
			dirPath := combinedPath + baseName
			dirSize, indexErr := idx.indexChildDirectory(ctx, dirPath)
			if indexErr != nil {
				if err := idx.handleChildDirError(dirPath, indexErr); err != nil {
					return IndexEntry{}, err
				}
				continue
			}
			totalSize += dirSize
			continue
		}

		fileSize, writeErr := idx.indexChildFile(ctx, file, normalizedDir, hidden)
		if writeErr != nil {
			return IndexEntry{}, writeErr
		}
		totalSize += fileSize
	}

	return IndexEntry{
		RelativePath: normalizedDir,
		Name:         filepath.Base(stat.Name()),
		Size:         totalSize,
		ModTime:      stat.ModTime(),
		Type:         "directory",
		Hidden:       dirHidden,
		Inode:        dirKey.ino,
	}, nil
}

func readableDirEntries(dir *os.File, combinedPath string) ([]os.FileInfo, error) {
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	files := make([]os.FileInfo, 0, len(entries))
	for _, entry := range entries {
		file, err := entry.Info()
		if err == nil {
			files = append(files, file)
			continue
		}
		path := combinedPath + entry.Name()
		if errors.Is(err, os.ErrNotExist) {
			slog.Debug("skipping deleted filesystem entry", "path", path, "err", err)
		} else {
			slog.Warn("cannot inspect filesystem entry; entry missing from scan", "path", path, "err", err)
		}
	}
	return files, nil
}

func (idx *Index) indexChildDirectory(ctx context.Context, dirPath string) (int64, error) {
	if err := idx.indexDirectory(ctx, dirPath); err != nil {
		return 0, err
	}

	dirMapKey := indexerapi.NormalizePath(dirPath)
	var size int64
	idx.mu.Lock()
	if meta, exists := idx.dirMetadata[dirMapKey]; exists {
		size = meta.Size
		delete(idx.dirMetadata, dirMapKey)
	}
	idx.mu.Unlock()
	return size, nil
}

func (idx *Index) indexChildFile(ctx context.Context, file os.FileInfo, normalizedDir string, hidden bool) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	size, contribution, key := idx.handleFile(file)
	idx.incrementFileCount()

	childPath := makeChildRelativePath(normalizedDir, file.Name())
	entry := IndexEntry{
		RelativePath:     childPath,
		Name:             file.Name(),
		Size:             int64(size),
		ModTime:          file.ModTime(),
		Type:             "file",
		Hidden:           hidden,
		Device:           key.dev,
		Inode:            key.ino,
		SizeContribution: int64(contribution),
	}
	if err := idx.streamWriter.Write(entry); err != nil {
		return 0, fmt.Errorf("%w: file %s: %v", ErrStreamWrite, childPath, err)
	}
	return int64(contribution), nil
}

func (idx *Index) handleChildDirError(dirPath string, err error) error {
	if errors.Is(err, ErrStreamWrite) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, ErrNotIndexed) || errors.Is(err, os.ErrNotExist) {
		slog.Debug("skipping directory", "path", dirPath, "err", err)
		return nil
	}
	idx.incrementSkippedDirs()
	slog.Warn("cannot index directory; subtree missing from scan", "path", dirPath, "err", err)
	return nil
}

func (idx *Index) handleFile(file os.FileInfo) (size, contribution uint64, key devIno) {
	var realSize uint64
	var nlink uint64 = 1
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
		if _, exists := idx.processedInodes[key]; exists {
			idx.mu.Unlock()
			return realSize, 0, key
		}
		idx.processedInodes[key] = struct{}{}
		idx.totalSize += realSize
		idx.mu.Unlock()
		return realSize, realSize, key
	}

	// It's a regular file.
	idx.mu.Lock()
	idx.totalSize += realSize
	idx.mu.Unlock()
	return realSize, realSize, key
}

func makeChildRelativePath(parent, child string) string {
	if parent == "/" {
		return filepath.Join("/", child)
	}
	parent = strings.TrimSuffix(parent, "/")
	return filepath.Join(parent, child)
}
