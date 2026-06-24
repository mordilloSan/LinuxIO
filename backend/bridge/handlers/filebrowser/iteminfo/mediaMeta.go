package iteminfo

import (
	"os"
	"strings"
)

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
