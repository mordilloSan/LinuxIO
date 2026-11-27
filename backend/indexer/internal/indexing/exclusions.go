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
// WithExcludePaths; network mounts, overlay mounts and Docker overlay "merged"
// views are fixed rules. Mount discovery itself is in mounts.go.

// overlayFSType is what a running container's root filesystem is mounted as.
// That mount shows again what its lower folders already hold, where Docker or
// Podman stores them, so a container's overlay is left out whatever the
// network setting and wherever the engine mounts it. Other overlays are part
// of the host and are indexed: a system extension over /usr, one an
// administrator put over a data folder, a root filesystem that is one.
const overlayFSType = "overlay"

// containerMountRoots are the folders the container engines mount under, and
// containerLayerMarks what the folders an overlay is made of are named when a
// container engine made them, wherever its data was moved to.
var (
	containerMountRoots = []string{
		"/var/lib/docker", "/var/lib/containers", "/var/lib/containerd",
		"/run/docker", "/run/containers", "/run/containerd",
	}
	containerLayerMarks = []string{"/overlay2/", "/containers/storage/overlay/", "io.containerd.snapshotter"}
)

// isContainerOverlay reports whether the overlay mounted at mountPoint with
// those options is a container's: it is mounted below a container engine's
// folder, or made of folders one keeps its layers in.
func isContainerOverlay(mountPoint, options string) bool {
	for _, root := range containerMountRoots {
		if strings.HasPrefix(mountPoint, root+"/") {
			return true
		}
	}
	for _, mark := range containerLayerMarks {
		if strings.Contains(options, mark) {
			return true
		}
	}
	return false
}

// dockerOverlayRoot holds overlay2 layers. Each layer's "merged" view
// duplicates content already present under its "diff" directory.
const dockerOverlayRoot = "/var/lib/docker/overlay2"

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
			if idx.isBelowMount(idx.externalMounts, fullCombined) || idx.isBelowMount(idx.overlayMounts, fullCombined) {
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

// isBelowMount reports whether fullCombined is one of the mount points or
// below one. The root is never a match.
func (idx *Index) isBelowMount(mountPoints map[string]string, fullCombined string) bool {
	if len(mountPoints) == 0 {
		return false
	}

	realPath := realPathFromCombined(idx.Path, fullCombined)
	if realPath == "" {
		return false
	}

	path := filepath.Clean(realPath)
	for mountPoint := range mountPoints {
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
