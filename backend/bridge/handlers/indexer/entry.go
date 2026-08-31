package indexer

import (
	"os"
	"path/filepath"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// EntryFromFileInfo builds the wire-level add request shared by filesystem
// producers. A negative size means use the size reported by info.
func EntryFromFileInfo(path string, info os.FileInfo, size int64) indexerapi.EntryRequest {
	if info == nil {
		return indexerapi.EntryRequest{}
	}

	absPath := utils.CleanAbsPath(path)
	name := filepath.Base(absPath)
	entryType := "file"
	if info.IsDir() {
		entryType = "directory"
	}
	if size < 0 {
		size = info.Size()
	}

	var inode uint64
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		inode = stat.Ino
	}

	return indexerapi.EntryRequest{
		Path:    utils.NormalizeIndexerPath(absPath),
		AbsPath: absPath,
		Name:    name,
		Size:    size,
		Type:    entryType,
		Hidden:  name != "." && name != ".." && len(name) > 0 && name[0] == '.',
		ModUnix: info.ModTime().Unix(),
		Inode:   inode,
	}
}
