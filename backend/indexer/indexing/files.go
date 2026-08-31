// Portions copyright 2018 File Browser contributors.
// Modified by LinuxIO.
// SPDX-License-Identifier: Apache-2.0

package indexing

import (
	"os"
	"syscall"
	"time"
)

// EntryFromFileInfo builds the canonical mutation payload from the filesystem.
func EntryFromFileInfo(path string, info os.FileInfo) IndexEntry {
	typ := "file"
	if info.IsDir() {
		typ = "directory"
	}
	return IndexEntry{
		RelativePath: NormalizeIndexPath(path),
		Name:         info.Name(),
		Size:         info.Size(),
		ModTime:      info.ModTime().In(time.UTC),
		Type:         typ,
		Hidden:       isHidden(info),
		Inode:        inodeFromFileInfo(info),
	}
}

func (idx *Index) handleFile(file os.FileInfo) (size uint64, shouldCountSize bool) {
	var realSize uint64
	var nlink uint64 = 1
	var key devIno
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
			return realSize, false
		}
		idx.processedInodes[key] = struct{}{}
		idx.totalSize += realSize
		idx.mu.Unlock()
		return realSize, true
	}

	// It's a regular file.
	idx.mu.Lock()
	idx.totalSize += realSize
	idx.mu.Unlock()
	return realSize, true // Count size.
}

func (idx *Index) incrementDirCount() {
	idx.mu.Lock()
	idx.NumDirs++
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

// SkippedDirCount reports how many directories were skipped because of
// permission or I/O errors (deliberate exclusions are not counted). A
// non-zero value means those subtrees are missing from the scan.
func (idx *Index) SkippedDirCount() uint64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.skippedDirs
}

func (idx *Index) incrementFileCount() {
	idx.mu.Lock()
	idx.NumFiles++
	idx.mu.Unlock()
}

func inodeFromFileInfo(info os.FileInfo) uint64 {
	if info == nil {
		return 0
	}
	_, _, key, ok := getFileDetails(info.Sys())
	if ok {
		return key.ino
	}
	return 0
}

func getFileDetails(sys any) (uint64, uint64, devIno, bool) {
	if stat, ok := sys.(*syscall.Stat_t); ok {
		// Use allocated size for `du`-like behavior
		realSize := uint64(stat.Blocks * 512)
		return realSize, stat.Nlink, devIno{dev: stat.Dev, ino: stat.Ino}, true
	}
	return 0, 1, devIno{}, false
}
