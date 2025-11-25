package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
// It lists files and folders but does NOT calculate recursive directory sizes
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
		// It's a file - basic info only
		fileInfo := &iteminfo.FileInfo{
			Path: adjustedPath,
			ItemInfo: iteminfo.ItemInfo{
				Name:    filepath.Base(realPath),
				Size:    dirStat.Size(),
				ModTime: dirStat.ModTime(),
				Type:    "file",
			},
		}
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
			if _, resolvedIsDir, simErr := iteminfo.ResolveSymlinks(fileRealPath); simErr == nil {
				isDir = resolvedIsDir
			}
		}

		itemInfo := &iteminfo.ItemInfo{
			Name:    entryName,
			ModTime: entry.ModTime(),
			Hidden:  hidden,
			Symlink: isSymlink,
		}

		if isDir {
			itemInfo.Type = "directory"
			dirInfos = append(dirInfos, *itemInfo)
		} else {
			itemInfo.Type = "file"
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
		Size:       totalSize, // Only sum of immediate files, not recursive
		ModTime:    dirStat.ModTime(),
		HasPreview: false,
	}
	dirFileInfo.SortItems()

	return dirFileInfo, nil
}

// processContent loads text content for small files only
func processContent(info *iteminfo.ExtendedFileInfo) {
	// Only load content for small files (editable text files)
	if info.Type == "directory" || info.Size >= 20*1024*1024 { // 20 megabytes
		return
	}

	content, err := GetContent(info.RealPath)
	if err != nil {
		return
	}
	info.Content = content
}
