package services

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
	"github.com/mordilloSan/go_logger/logger"
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
func CalculateDirectorySize(root string) (*DirectoryStats, error) {
	stats := &DirectoryStats{}

	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = root
	}

	// count root
	stats.FolderCount.Add(1)

	// bump workers a bit
	workerCount := runtime.NumCPU() * 6

	// Very large channel buffer to avoid blocking on sends
	// Most filesystems won't have more directories than this at once
	dirCh := make(chan string, 65536)

	// hardlink dedupe
	visited := make(map[uint64]bool)
	var visitedMu sync.Mutex

	// how many dirs are "in flight"
	var pending sync.WaitGroup
	pending.Add(1) // root

	var workers sync.WaitGroup
	workers.Add(workerCount)

	for i := 0; i < workerCount; i++ {
		go func() {
			defer workers.Done()
			defer func() {
				if r := recover(); r != nil {
					logger.Warnf("recovered from panic in directory size worker: %v", r)
				}
			}()
			for dir := range dirCh {
				processDir(dir, stats, dirCh, &pending, visited, &visitedMu)
				// finished this dir
				pending.Done()
			}
		}()
	}

	// seed
	dirCh <- realRoot

	// closer: when pending hits 0, we can close channel
	go func() {
		pending.Wait()
		close(dirCh)
	}()

	// wait all workers
	workers.Wait()

	return stats, nil
}

func processDir(
	dirPath string,
	stats *DirectoryStats,
	dirCh chan<- string,
	pending *sync.WaitGroup,
	visited map[uint64]bool,
	visitedMu *sync.Mutex,
) {
	defer func() {
		if r := recover(); r != nil {
			logger.Warnf("recovered from panic in processDir(%s): %v", dirPath, r)
		}
	}()

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		// Silently continue on permission denied or other read errors
		return
	}

	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		if entry.IsDir() {
			stats.FolderCount.Add(1)
			sub := filepath.Join(dirPath, entry.Name())
			pending.Add(1)

			// Non-blocking send with fallback: either send directly or spawn helper
			select {
			case dirCh <- sub:
				// Successfully sent to channel
			default:
				// Channel full, spawn helper goroutine to avoid blocking
				go func(path string) {
					dirCh <- path
				}(sub)
			}
			continue
		}

		info, err := entry.Info()
		if err != nil {
			stats.FileCount.Add(1)
			continue
		}

		stats.FileCount.Add(1)

		// get stat_t to read Blocks
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			// fallback to logical size
			stats.TotalSize.Add(info.Size())
			continue
		}

		// hardlink fast-path – this part can stay
		if st.Nlink > 1 {
			inode := st.Ino
			visitedMu.Lock()
			if !visited[inode] {
				visited[inode] = true
				// use on-disk size, not logical size
				diskBytes := st.Blocks * 512
				stats.TotalSize.Add(diskBytes)
			}
			visitedMu.Unlock()
			continue
		}

		// normal file: use on-disk size
		diskBytes := st.Blocks * 512
		stats.TotalSize.Add(diskBytes)
	}
}
