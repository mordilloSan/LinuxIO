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
	"time"

	"github.com/mordilloSan/go_logger/logger"

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

// IMPORTANT!
// we do this to prevent multiple calls that would just blow up and kill the bridge.

// CachedDirectoryStats holds cached size info with timestamp
type CachedDirectoryStats struct {
	stats     *DirectoryStats
	timestamp time.Time
}

// DirectorySizeCache manages caching of directory size calculations with deduplication
type DirectorySizeCache struct {
	mu       sync.Mutex
	cache    map[string]*CachedDirectoryStats
	inFlight map[string]chan struct{} // path -> "done" signal
	cacheTTL time.Duration
}

var dirSizeCache = &DirectorySizeCache{
	cache:    make(map[string]*CachedDirectoryStats),
	inFlight: make(map[string]chan struct{}),
	cacheTTL: 10 * time.Second,
}

func getOrCalculateDirectorySize(root string) (*DirectoryStats, error) {
	dirSizeCache.mu.Lock()

	// 1) check fresh cache
	if cached, exists := dirSizeCache.cache[root]; exists {
		if time.Since(cached.timestamp) < dirSizeCache.cacheTTL {
			dirSizeCache.mu.Unlock()
			logger.Debugf("cache hit for %s", root)
			return cached.stats, nil
		}
		// expired
		delete(dirSizeCache.cache, root)
	}

	// 2) is someone already computing this?
	if ch, inFlight := dirSizeCache.inFlight[root]; inFlight {
		// wait for them to finish
		dirSizeCache.mu.Unlock()
		logger.Debugf("waiting for in-flight scan of %s", root)
		<-ch // will unblock when the channel is closed

		// after it’s done, read from cache
		dirSizeCache.mu.Lock()
		cached := dirSizeCache.cache[root]
		dirSizeCache.mu.Unlock()

		if cached != nil {
			return cached.stats, nil
		}
		// if we get here, the first computation failed and didn’t cache
		return nil, fmt.Errorf("directory scan for %s finished without cached result", root)
	}

	// 3) we are the first -> create signal channel
	doneCh := make(chan struct{})
	dirSizeCache.inFlight[root] = doneCh
	dirSizeCache.mu.Unlock()

	// ---- do the heavy work outside the lock ----
	stats, err := calculateDirectorySizeImpl(root)

	// ---- now publish result ----
	dirSizeCache.mu.Lock()
	if err == nil && stats != nil {
		dirSizeCache.cache[root] = &CachedDirectoryStats{
			stats:     stats,
			timestamp: time.Now(),
		}
	}
	// remove from inFlight and close to wake ALL waiters
	delete(dirSizeCache.inFlight, root)
	close(doneCh)
	dirSizeCache.mu.Unlock()

	return stats, err
}

// CalculateDirectorySize is the public entry point that uses caching
func CalculateDirectorySize(root string) (*DirectoryStats, error) {
	return getOrCalculateDirectorySize(root)
}

// calculateDirectorySizeImpl recursively calculates the total size of a directory
// and counts files and folders using faster os.ReadDir instead of filepath.Walk
// Resolves symlinks in the input path so users can explicitly request symlinked directories
func calculateDirectorySizeImpl(root string) (*DirectoryStats, error) {
	stats := &DirectoryStats{}

	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = root
	}

	// count root
	stats.FolderCount.Add(1)

	// Limit concurrent workers to avoid file descriptor exhaustion
	// and memory pressure from too many goroutines
	workerCount := runtime.NumCPU()
	if workerCount < 4 {
		workerCount = 4
	}
	if workerCount > 16 {
		workerCount = 16
	}

	// Large channel buffer to queue directories for workers
	dirCh := make(chan string, 8192)

	// Semaphore to limit concurrent os.ReadDir calls
	// Prevents file descriptor exhaustion
	semaphore := make(chan struct{}, workerCount*2)

	// hardlink dedupe
	visited := make(map[uint64]bool)
	var visitedMu sync.Mutex

	// Track closure: when pending hits 0, we can close channel
	closeDone := make(chan struct{})
	var closing atomic.Bool

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
				processDir(dir, stats, dirCh, &pending, visited, &visitedMu, &closing, semaphore)
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
		closing.Store(true)
		close(closeDone)
	}()

	// Wait for closer to signal, then close channel
	go func() {
		<-closeDone
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
	closing *atomic.Bool,
	semaphore chan struct{},
) {
	defer func() {
		if r := recover(); r != nil {
			logger.Warnf("recovered from panic in processDir(%s): %v", dirPath, r)
		}
	}()

	// Acquire semaphore slot to limit concurrent directory reads
	semaphore <- struct{}{}
	defer func() { <-semaphore }()

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
				// Only if we're not shutting down
				if !closing.Load() {
					go func(path string) {
						defer func() {
							if r := recover(); r != nil {
								logger.Warnf("recovered from panic in helper goroutine for %s: %v", path, r)
								pending.Done()
							}
						}()
						// Double-check we're not closing before sending
						if !closing.Load() {
							dirCh <- path
						} else {
							pending.Done()
						}
					}(sub)
				} else {
					// Already closing, don't queue
					pending.Done()
				}
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
