package virt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const vncSocketDir = "/var/lib/libvirt/qemu"

// statHostFile trims rawPath and requires it to be an existing absolute path.
// field names the request field and label the path in error messages.
func statHostFile(rawPath, field, label string) (string, os.FileInfo, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return "", nil, badRequestf("%s is required", field)
	}
	if !filepath.IsAbs(path) {
		return "", nil, badRequestf("%s path must be absolute", label)
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, notFoundf("%s path does not exist: %s", label, path)
		}
		return "", nil, fmt.Errorf("%s path is not accessible: %w", label, err)
	}
	return path, info, nil
}

func isSafeVNCSocket(path string) bool {
	return strings.HasPrefix(path, vncSocketDir+"/") && !strings.Contains(path, "..")
}

func qemuReadable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
