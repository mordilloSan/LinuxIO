package iteminfo

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
)

// DetectTypeByHeader detects the MIME type of a file based on its header.
func DetectTypeByHeader(realPath string) string {
	root, err := fsroot.Open()
	if err != nil {
		return "blob"
	}
	defer root.Close()

	cleanPath := filepath.Clean("/" + strings.TrimPrefix(realPath, "/"))
	relPath := fsroot.ToRel(cleanPath)

	// First, check if it's a regular file
	fileInfo, err := root.Root.Stat(relPath)
	if err != nil {
		return "blob"
	}

	// Skip type detection for non-regular files (devices, pipes, sockets, etc.)
	if !fileInfo.Mode().IsRegular() {
		return "blob"
	}

	file, err := root.Root.Open(relPath)
	if err != nil {
		return "blob"
	}
	defer file.Close()

	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil {
		return "blob"
	}
	return http.DetectContentType(buffer[:n])
}

// IsDirectory determines if a path should be treated as a directory.
// It treats known bundle-style directories as files instead.
func IsDirectory(fileInfo os.FileInfo) bool {
	if !fileInfo.IsDir() {
		return false
	}

	name := strings.ToLower(fileInfo.Name())
	bundleSuffixes := []string{
		".app",
		".bundle",
		".pkg",
		".framework",
	}
	for _, suffix := range bundleSuffixes {
		if strings.HasSuffix(name, suffix) {
			return false
		}
	}
	return true
}
