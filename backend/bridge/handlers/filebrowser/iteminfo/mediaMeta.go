package iteminfo

import (
	"net/http"
	"os"
)

// DetectTypeByHeader detects the MIME type of a file based on its header.
func DetectTypeByHeader(realPath string) string {
	// First, check if it's a regular file
	fileInfo, err := os.Stat(realPath)
	if err != nil {
		return "blob"
	}

	// Skip type detection for non-regular files (devices, pipes, sockets, etc.)
	if !fileInfo.Mode().IsRegular() {
		return "blob"
	}
	file, err := os.Open(realPath)
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

	// For bundle-type dirs, treat them as files
	return false
}
