package indexing

import (
	"bufio"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	externalFSTypes = map[string]struct{}{
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
	// loadExternalMountPointsFn is swappable in tests.
	loadExternalMountPointsFn = loadExternalMountPoints
)

// isDockerOverlayMergedPath returns true for Docker overlay2 merged mount views.
// These paths duplicate content already present under overlay2/*/diff.
func (idx *Index) isDockerOverlayMergedPath(fullCombined string) bool {
	realPath := idx.realPathFromCombined(fullCombined)
	if realPath == "" {
		return false
	}

	const dockerOverlayRoot = "/var/lib/docker/overlay2"
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

func (idx *Index) isExternalMount(fullCombined string) bool {
	if len(idx.externalMounts) == 0 {
		return false
	}

	realPath := idx.realPathFromCombined(fullCombined)
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

func loadExternalMountPoints() map[string]string {
	mounts := make(map[string]string)
	if runtime.GOOS != "linux" {
		return mounts
	}

	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		slog.Warn("unable to read mountinfo", "err", err)
		return mounts
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			slog.Warn("unable to close mountinfo file", "err", closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		mountPoint, fsType, source, ok := parseMountInfo(scanner.Text())
		if !ok {
			continue
		}
		if isExternalFilesystem(fsType, source) {
			mounts[mountPoint] = fsType
		}
	}
	if err := scanner.Err(); err != nil {
		slog.Warn("error while scanning mountinfo", "err", err)
	}
	return mounts
}

func parseMountInfo(line string) (mountPoint, fsType, source string, ok bool) {
	parts := strings.Split(line, " - ")
	if len(parts) != 2 {
		return "", "", "", false
	}

	pre := strings.Fields(parts[0])
	post := strings.Fields(parts[1])
	if len(pre) < 5 || len(post) < 2 {
		return "", "", "", false
	}

	rawMountPoint := pre[4]
	mountPoint = decodeMountPath(rawMountPoint)
	mountPoint = filepath.Clean(mountPoint)
	fsType = strings.ToLower(post[0])
	source = strings.ToLower(post[1])
	return mountPoint, fsType, source, true
}

func decodeMountPath(raw string) string {
	decoded := strings.ReplaceAll(raw, "\\040", " ")
	decoded = strings.ReplaceAll(decoded, "\\011", "\t")
	decoded = strings.ReplaceAll(decoded, "\\012", "\n")
	decoded = strings.ReplaceAll(decoded, "\\134", "\\")
	return decoded
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
