package virt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var validateInstallMedia = validateInstallMediaPath

func validateInstallMediaPath(rawPath string) (string, error) {
	isoPath := strings.TrimSpace(rawPath)
	if isoPath == "" {
		return "", badRequestf("isoPath is required")
	}
	if !filepath.IsAbs(isoPath) {
		return "", badRequestf("ISO path must be absolute")
	}

	info, err := os.Stat(isoPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", notFoundf("ISO path does not exist: %s", isoPath)
		}
		return "", fmt.Errorf("ISO path is not accessible: %w", err)
	}
	if info.IsDir() {
		return "", badRequestf("ISO path must point to a regular .iso file, not a directory: %s", isoPath)
	}
	if !info.Mode().IsRegular() {
		return "", badRequestf("ISO path must point to a regular .iso file: %s", isoPath)
	}
	if !strings.EqualFold(filepath.Ext(isoPath), ".iso") {
		return "", badRequestf("ISO path must point to a .iso file: %s", isoPath)
	}
	if !qemuReadable(isoPath) {
		return "", badRequestf("ISO path is not readable by the bridge process: %s", isoPath)
	}
	return isoPath, nil
}
