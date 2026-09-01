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
	"runtime"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing/iteminfo"
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
	hidden := isHidden(dirInfo)
	if idx.shouldSkip(dirInfo.IsDir(), hidden, adjustedPath) {
		return ErrNotIndexed
	}

	dirFileInfo, err2 := idx.GetDirInfo(ctx, dir, dirInfo, realPath, adjustedPath)
	if err2 != nil {
		return err2
	}

	idx.mu.Lock()
	dirKey := NormalizeIndexPath(adjustedPath)
	idx.dirMetadata[dirKey] = DirMetadata{
		Size:    dirFileInfo.Size,
		ModTime: dirFileInfo.ModTime,
		Inode:   dirFileInfo.Inode,
	}
	idx.mu.Unlock()

	normalized := dirKey
	entry := IndexEntry{
		RelativePath: normalized,
		Name:         directoryName(dirFileInfo, normalized),
		Size:         dirFileInfo.Size,
		ModTime:      dirFileInfo.ModTime,
		Type:         "directory",
		Hidden:       dirFileInfo.Hidden,
		Inode:        dirFileInfo.Inode,
	}
	if err := idx.streamWriter.Write(entry); err != nil {
		return fmt.Errorf("%w: directory %s: %v", ErrStreamWrite, normalized, err)
	}
	idx.incrementDirCount()
	return nil
}

func (idx *Index) GetDirInfo(ctx context.Context, dirInfo *os.File, stat os.FileInfo, realPath, adjustedPath string) (*iteminfo.FileInfo, error) {
	// Ensure combinedPath has exactly one trailing slash to prevent double slashes in subdirectory paths
	combinedPath := strings.TrimRight(adjustedPath, "/") + "/"
	// Read directory contents
	files, err := dirInfo.Readdir(-1)
	if err != nil {
		return nil, err
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
	normalizedDir := NormalizeIndexPath(adjustedPath)

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hidden := isHidden(file)
		isDir := file.IsDir()
		baseName := file.Name()
		fullCombined := combinedPath + baseName
		if idx.shouldSkip(isDir, hidden, fullCombined) {
			continue
		}

		if isDir {
			dirPath := combinedPath + baseName
			dirSize, indexErr := idx.indexChildDirectory(ctx, dirPath)
			if indexErr != nil {
				if err := idx.handleChildDirError(dirPath, indexErr); err != nil {
					return nil, err
				}
				continue
			}
			totalSize += dirSize
			continue
		}

		fileSize, writeErr := idx.indexChildFile(ctx, file, normalizedDir, hidden)
		if writeErr != nil {
			return nil, writeErr
		}
		totalSize += fileSize
	}

	dirFileInfo := &iteminfo.FileInfo{
		Path: adjustedPath,
	}
	dirFileInfo.ItemInfo = iteminfo.ItemInfo{
		Name:    filepath.Base(stat.Name()),
		Type:    "directory",
		Size:    totalSize,
		ModTime: stat.ModTime(),
		Inode:   dirKey.ino,
		Hidden:  dirHidden,
	}
	return dirFileInfo, nil
}

func (idx *Index) indexChildDirectory(ctx context.Context, dirPath string) (int64, error) {
	if err := idx.indexDirectory(ctx, dirPath); err != nil {
		return 0, err
	}

	dirMapKey := NormalizeIndexPath(dirPath)
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
	return fmt.Errorf("index directory %s: %w", dirPath, err)
}

func isHidden(file os.FileInfo) bool {
	// Check if the file starts with a dot (Linux hidden files)
	name := file.Name()
	return len(name) > 0 && name[0] == '.'
}

func (idx *Index) shouldSkip(isDir bool, isHidden bool, fullCombined string) bool {
	if fullCombined == "/" {
		return false
	}

	if isDir {
		if IsPathExcluded(idx.Path, idx.excludePaths, fullCombined) {
			return true
		}

		if runtime.GOOS == "linux" {
			if !idx.includeNetworkMounts && idx.isExternalMount(fullCombined) {
				return true
			}
			if idx.isDockerOverlayMergedPath(fullCombined) {
				return true
			}
		}
	}

	return false
}

func realPathFromCombined(indexPath, fullCombined string) string {
	base := filepath.Clean(indexPath)
	if base == "" {
		base = "/"
	}

	cleanCombined := filepath.Clean(fullCombined)
	if cleanCombined == "/" || cleanCombined == "." {
		return base
	}

	relative := strings.TrimPrefix(cleanCombined, "/")
	return filepath.Join(base, relative)
}
