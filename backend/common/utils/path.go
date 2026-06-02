package utils

import (
	"os"
	"path/filepath"
	"strings"
)

func CleanAbsPath(path string) string {
	if path == "" {
		return "/"
	}
	return filepath.Clean("/" + strings.TrimPrefix(path, "/"))
}

func NormalizeIndexerPath(path string) string {
	if path == "" || path == "/" {
		return "/"
	}
	path = strings.TrimRight(path, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
