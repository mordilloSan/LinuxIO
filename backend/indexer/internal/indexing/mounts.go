package indexing

import (
	"bufio"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/octalescape"
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

	mountPoint = filepath.Clean(octalescape.Unescape(pre[4]))
	fsType = strings.ToLower(post[0])
	source = strings.ToLower(post[1])
	return mountPoint, fsType, source, true
}
