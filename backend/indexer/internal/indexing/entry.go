// Portions copyright 2018 File Browser contributors.
// Modified by LinuxIO.
// SPDX-License-Identifier: Apache-2.0

package indexing

import (
	"os"
	"syscall"
	"time"

	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// IndexEntry captures a flattened representation of either a directory or a file.
type IndexEntry struct {
	RelativePath string
	Name         string
	Size         int64
	ModTime      time.Time
	Type         string // "directory" or "file"
	Hidden       bool
	Device       uint64
	Inode        uint64
	// SizeContribution is the part of Size counted in ancestor aggregates.
	// It is zero for duplicate paths to the same hard-linked allocation.
	SizeContribution int64
}

// EntryFromFileInfo builds the canonical mutation payload from the filesystem.
func EntryFromFileInfo(path string, info os.FileInfo) IndexEntry {
	typ := "file"
	if info.IsDir() {
		typ = "directory"
	}
	size := info.Size()
	var key devIno
	if allocatedSize, _, fileKey, ok := getFileDetails(info.Sys()); ok {
		size = int64(allocatedSize)
		key = fileKey
	}
	return IndexEntry{
		RelativePath: indexerapi.NormalizePath(path),
		Name:         info.Name(),
		Size:         size,
		ModTime:      info.ModTime().In(time.UTC),
		Type:         typ,
		Hidden:       isHidden(info),
		Device:       key.dev,
		Inode:        key.ino,
	}
}

// SameFile reports whether path currently names a non-directory with the given
// device and inode. The storage layer uses it to ignore stored hardlink paths
// that no longer match the filesystem.
func SameFile(path string, device, inode int64) bool {
	info, err := os.Lstat(path)
	if err != nil || info.IsDir() {
		return false
	}
	entry := EntryFromFileInfo(path, info)
	return entry.Type != "directory" && int64(entry.Device) == device && int64(entry.Inode) == inode
}

func getFileDetails(sys any) (uint64, uint64, devIno, bool) {
	if stat, ok := sys.(*syscall.Stat_t); ok {
		// Use allocated size for `du`-like behavior
		realSize := uint64(stat.Blocks * 512)
		return realSize, stat.Nlink, devIno{dev: stat.Dev, ino: stat.Ino}, true
	}
	return 0, 1, devIno{}, false
}

func isHidden(file os.FileInfo) bool {
	// Check if the file starts with a dot (Linux hidden files)
	name := file.Name()
	return len(name) > 0 && name[0] == '.'
}
