// Portions copyright 2018 File Browser contributors.
// Modified by LinuxIO.
// SPDX-License-Identifier: Apache-2.0

package indexing

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Exclusion policy: everything that decides whether a path is left out of the
// index lives here. Operator and mandatory paths arrive through
// WithExcludePaths; network mounts and Docker overlay "merged" views are fixed
// rules. Mount discovery itself is in mounts.go.

// dockerOverlayRoot holds overlay2 layers. Each layer's "merged" view
// duplicates content already present under its "diff" directory.
const dockerOverlayRoot = "/var/lib/docker/overlay2"

var externalFSTypes = map[string]struct{}{
	"nfs":        {},
	"nfs4":       {},
	"cifs":       {},
	"smbfs":      {},
	"smb2":       {},
	"smb3":       {},
	"fuse.cifs":  {},
	"fuse.smb":   {},
	"fuse.smb3":  {},
	"fuse.nfs":   {},
	"fuse.ceph":  {},
	"fuse.iscsi": {},
}

func isExternalFilesystem(fsType, source string) bool {
	if _, ok := externalFSTypes[fsType]; ok {
		return true
	}
	if strings.Contains(fsType, "iscsi") || strings.Contains(source, "iscsi") {
		return true
	}
	if strings.HasPrefix(source, "//") {
		return true
	}
	return false
}

// shouldSkip reports whether traversal leaves fullCombined out of the index.
// Only directories are excluded; the root never is.
func (idx *Index) shouldSkip(isDir bool, fullCombined string) bool {
	if fullCombined == "/" {
		return false
	}

	if isDir {
		if IsPathExcluded(idx.Path, idx.excludePaths, fullCombined) {
			return true
		}

		if runtime.GOOS == "linux" {
			if !idx.includeNetworkMounts && idx.isExternalMount(fullCombined) {
				return true
			}
			if idx.isDockerOverlayMergedPath(fullCombined) {
				return true
			}
		}
	}

	return false
}

// IsPathExcluded reports whether an index-relative path is inside one of the
// configured absolute filesystem paths.
func IsPathExcluded(indexPath string, excludedPaths []string, relativePath string) bool {
	realPath := realPathFromCombined(indexPath, relativePath)
	if realPath == "" {
		return false
	}

	for _, excludedPath := range excludedPaths {
		excludedPath = filepath.Clean(excludedPath)
		if realPath == excludedPath {
			return true
		}
		if strings.HasPrefix(realPath, excludedPath+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// IsPathExcludedFromIndex applies the same configured, mount, and Docker
// subtree exclusions used by traversal to a direct mutation path.
func IsPathExcludedFromIndex(indexPath string, excludedPaths []string, includeNetworkMounts bool, relativePath string) bool {
	idx := newIndex(
		indexPath,
		WithNetworkMounts(includeNetworkMounts),
		WithExcludePaths(excludedPaths),
	)
	return idx.shouldSkip(true, relativePath)
}

func (idx *Index) isExternalMount(fullCombined string) bool {
	if len(idx.externalMounts) == 0 {
		return false
	}

	realPath := realPathFromCombined(idx.Path, fullCombined)
	if realPath == "" {
		return false
	}

	path := filepath.Clean(realPath)
	for mountPoint := range idx.externalMounts {
		if mountPoint == "" || mountPoint == "/" {
			continue
		}
		if path == mountPoint || strings.HasPrefix(path, mountPoint+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// isDockerOverlayMergedPath returns true for Docker overlay2 merged mount views.
// These paths duplicate content already present under overlay2/*/diff.
func (idx *Index) isDockerOverlayMergedPath(fullCombined string) bool {
	realPath := realPathFromCombined(idx.Path, fullCombined)
	if realPath == "" {
		return false
	}

	cleanPath := filepath.Clean(realPath)
	if !strings.HasPrefix(cleanPath, dockerOverlayRoot+string(os.PathSeparator)) {
		return false
	}

	// Expect: /var/lib/docker/overlay2/<layer-id>/merged[/...]
	rest := strings.TrimPrefix(cleanPath, dockerOverlayRoot+string(os.PathSeparator))
	parts := strings.Split(rest, string(os.PathSeparator))
	if len(parts) < 2 {
		return false
	}

	return parts[1] == "merged"
}

func realPathFromCombined(indexPath, fullCombined string) string {
	base := filepath.Clean(indexPath)
	if base == "" {
		base = "/"
	}

	cleanCombined := filepath.Clean(fullCombined)
	if cleanCombined == "/" || cleanCombined == "." {
		return base
	}

	relative := strings.TrimPrefix(cleanCombined, "/")
	return filepath.Join(base, relative)
}
