package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
)

// FileInfoFaster retrieves file/directory information quickly
func FileInfoFaster(opts iteminfo.FileOptions) (*iteminfo.ExtendedFileInfo, error) {
	response := &iteminfo.ExtendedFileInfo{}

	if !strings.HasPrefix(opts.Path, "/") {
		opts.Path = "/" + opts.Path
	}

	// Build real path directly
	realPath := filepath.Join(opts.Path)

	// Resolve symlinks
	resolvedPath, isDir, err := iteminfo.ResolveSymlinks(realPath)
	if err != nil {
		return response, fmt.Errorf("could not resolve path: %v, error: %v", opts.Path, err)
	}

	if !strings.HasSuffix(opts.Path, "/") && isDir {
		opts.Path = opts.Path + "/"
	}

	var info *iteminfo.FileInfo
	if isDir {
		info, err = GetDirInfo(opts.Path, resolvedPath)
		if err != nil {
			return response, err
		}
	} else {
		// For files, get info from parent directory
		parentPath := filepath.Dir(opts.Path)
		if parentPath == "." {
			parentPath = "/"
		}
		parentRealPath := filepath.Dir(resolvedPath)

		dirInfo, err := GetDirInfo(parentPath, parentRealPath)
		if err != nil {
			return response, err
		}

		// Find the file in the parent directory
		baseName := filepath.Base(resolvedPath)
		for _, file := range dirInfo.Files {
			if file.Name == baseName {
				info = &iteminfo.FileInfo{
					Path:     opts.Path,
					ItemInfo: file,
				}
				break
			}
		}
		if info == nil {
			return response, fmt.Errorf("file not found: %s", opts.Path)
		}
	}

	response.FileInfo = *info
	response.RealPath = resolvedPath

	if opts.Content || opts.Metadata {
		processContent(response)
	}
	return response, nil
}

// GetDirInfo retrieves information about a directory and its contents
func GetDirInfo(adjustedPath, realPath string) (*iteminfo.FileInfo, error) {
	dir, err := os.Open(realPath)
	if err != nil {
		return nil, err
	}
	defer dir.Close()

	dirStat, err := dir.Stat()
	if err != nil {
		return nil, err
	}

	if !dirStat.IsDir() {
		// It's a file
		fileInfo := &iteminfo.FileInfo{
			Path: adjustedPath,
			ItemInfo: iteminfo.ItemInfo{
				Name:    filepath.Base(realPath),
				Size:    dirStat.Size(),
				ModTime: dirStat.ModTime(),
			},
		}
		fileInfo.DetectType(realPath)
		setFilePreviewFlags(&fileInfo.ItemInfo)
		return fileInfo, nil
	}

	// Read directory contents
	entries, err := dir.Readdir(-1)
	if err != nil {
		return nil, err
	}

	var totalSize int64
	fileInfos := []iteminfo.ItemInfo{}
	dirInfos := []iteminfo.ItemInfo{}

	for _, entry := range entries {
		entryName := entry.Name()
		hidden := entryName[0] == '.'
		isDir := entry.IsDir()
		fileRealPath := filepath.Join(realPath, entryName)
		isSymlink := entry.Mode()&os.ModeSymlink != 0

		// Handle symlinks
		if !isDir && isSymlink {
			if resolvedPath, resolvedIsDir, simErr := iteminfo.ResolveSymlinks(fileRealPath); simErr == nil {
				isDir = resolvedIsDir
				if resolvedIsDir {
					fileRealPath = resolvedPath
				}
			}
		}

		itemInfo := &iteminfo.ItemInfo{
			Name:    entryName,
			ModTime: entry.ModTime(),
			Hidden:  hidden,
			Symlink: isSymlink,
		}

		if isDir {
			// Skip recursive size calculation for directories to keep listings fast
			itemInfo.Size = 0
			itemInfo.HasPreview = false
			itemInfo.Type = "directory"
			dirInfos = append(dirInfos, *itemInfo)
		} else {
			itemInfo.DetectType(fileRealPath)
			setFilePreviewFlags(itemInfo)
			itemInfo.Size = entry.Size()
			fileInfos = append(fileInfos, *itemInfo)
			totalSize += itemInfo.Size
		}
	}

	dirFileInfo := &iteminfo.FileInfo{
		Path:    adjustedPath,
		Files:   fileInfos,
		Folders: dirInfos,
	}
	dirFileInfo.ItemInfo = iteminfo.ItemInfo{
		Name:       filepath.Base(realPath),
		Type:       "directory",
		Size:       totalSize,
		ModTime:    dirStat.ModTime(),
		HasPreview: false,
	}
	dirFileInfo.SortItems()

	return dirFileInfo, nil
}

// setFilePreviewFlags determines if a file can be previewed
func setFilePreviewFlags(fileInfo *iteminfo.ItemInfo) {
	simpleType := strings.Split(fileInfo.Type, "/")[0]

	// Check if it's an image
	if simpleType == "image" {
		fileInfo.HasPreview = true
	}

	if iteminfo.HasDocConvertableExtension(fileInfo.Name, fileInfo.Type) {
		fileInfo.HasPreview = true
	}
}

// processContent determines what content to include in the response
func processContent(info *iteminfo.ExtendedFileInfo) {
	isVideo := strings.HasPrefix(info.Type, "video")
	isAudio := strings.HasPrefix(info.Type, "audio")
	isFolder := info.Type == "directory"
	if isFolder {
		return
	}

	if isVideo {
		return
	}

	// Audio files no longer have preview capability (playback removed)
	if isAudio {
		return
	}

	// Process text content for non-video, non-audio files
	if info.Size < 20*1024*1024 { // 20 megabytes in bytes
		content, err := GetContent(info.RealPath)
		if err != nil {
			return
		}
		info.Content = content
	}
}

// DirectoryStats contains statistics about a directory
type DirectoryStats struct {
	TotalSize   atomic.Int64
	FileCount   atomic.Int64
	FolderCount atomic.Int64
}

// CalculateDirectorySize recursively calculates the total size of a directory
// and counts files and folders using faster os.ReadDir instead of filepath.Walk
// Resolves symlinks in the input path so users can explicitly request symlinked directories
func CalculateDirectorySize(path string) (*DirectoryStats, error) {
	stats := &DirectoryStats{}
	var wg sync.WaitGroup
	visitedInodes := make(map[uint64]bool)
	var inodeMu sync.Mutex

	// Resolve symlinks in the input path (allows users to explicitly traverse symlinked dirs)
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		// If EvalSymlinks fails, use the original path
		realPath = path
	}

	calculateDirSizeRecursive(realPath, stats, &wg, visitedInodes, &inodeMu)
	wg.Wait()
	return stats, nil
}

// calculateDirSizeRecursive is a helper function that recursively calculates directory stats
// Uses os.ReadDir which is faster than filepath.Walk
// IMPORTANT: Only calls entry.Info() for files, not directories (huge performance improvement)
// Also skips symlinks to avoid loops and double-counting
// Uses goroutines to parallelize subdirectory traversal for maximum performance
// Uses atomic operations for lock-free counting (much faster than mutexes)
// Gracefully handles permission errors - counts inaccessible files/folders anyway (like `find` does)
// Tracks inodes to avoid double-counting hardlinks
func calculateDirSizeRecursive(path string, stats *DirectoryStats, wg *sync.WaitGroup, visitedInodes map[uint64]bool, inodeMu *sync.Mutex) {
	entries, err := os.ReadDir(path)
	if err != nil {
		// Permission denied or other error reading directory
		// Still try to stat the directory itself to count it
		if _, err := os.Stat(path); err == nil {
			stats.FolderCount.Add(1)
		}
		return
	}

	for _, entry := range entries {
		// Skip symlinks to avoid infinite loops and double-counting
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		if entry.IsDir() {
			// For directories, count it (atomic - no lock needed)
			stats.FolderCount.Add(1)

			// Recursively process subdirectories in parallel
			subPath := filepath.Join(path, entry.Name())
			wg.Add(1)
			go func(p string) {
				defer wg.Done()
				calculateDirSizeRecursive(p, stats, wg, visitedInodes, inodeMu)
			}(subPath)
		} else {
			// For files, we need the size - only call entry.Info() here
			info, err := entry.Info()
			if err != nil {
				// Permission denied on file - count it with 0 size (atomic - no lock needed)
				stats.FileCount.Add(1)
				continue
			}

			// Count the file (atomic - no lock needed)
			stats.FileCount.Add(1)

			// Track by inode to avoid double-counting size (for hardlinks and deduplication)
			var inode uint64
			if sys := info.Sys(); sys != nil {
				if stat, ok := sys.(*syscall.Stat_t); ok {
					inode = stat.Ino
				}
			}

			if inode > 0 {
				inodeMu.Lock()
				alreadyCounted := visitedInodes[inode]
				if !alreadyCounted {
					visitedInodes[inode] = true
					// Only count size for first occurrence of this inode
					stats.TotalSize.Add(info.Size())
				}
				inodeMu.Unlock()
			} else {
				// If we can't get inode, count the size (safer fallback)
				stats.TotalSize.Add(info.Size())
			}
		}
	}
}
