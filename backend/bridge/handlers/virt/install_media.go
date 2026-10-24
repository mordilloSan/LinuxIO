package virt

import (
	"path/filepath"
	"strings"
)

var validateInstallMedia = validateInstallMediaPath

func validateInstallMediaPath(rawPath string) (string, error) {
	isoPath, info, err := statHostFile(rawPath, "isoPath", "ISO")
	if err != nil {
		return "", err
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
