package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/server/filebrowser/iteminfo"
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
