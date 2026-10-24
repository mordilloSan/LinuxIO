package indexing

import (
	"bufio"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// loadExternalMountPointsFn is swappable in tests.
var loadExternalMountPointsFn = loadExternalMountPoints

// loadExternalMountPoints reads /proc/self/mountinfo and returns the mount
// points of network and iSCSI filesystems, keyed to their filesystem type.
func loadExternalMountPoints() map[string]string {
	mounts := make(map[string]string)
	if runtime.GOOS != "linux" {
		return mounts
	}

	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		slog.Warn("unable to read mountinfo", "error", err)
		return mounts
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			slog.Warn("unable to close mountinfo file", "error", closeErr)
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
		slog.Warn("error while scanning mountinfo", "error", err)
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
