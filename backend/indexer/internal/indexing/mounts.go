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

// loadMountPointsFn is swappable in tests.
var loadMountPointsFn = loadMountPoints

// loadMountPoints reads /proc/self/mountinfo and returns the mount points the
// walk may leave out, keyed to their filesystem type: network and iSCSI
// filesystems, and the overlay mounts of containers.
func loadMountPoints() map[string]string {
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
		mountPoint, fsType, source, options, ok := parseMountInfo(scanner.Text())
		if !ok {
			continue
		}
		if fsType == overlayFSType {
			if isContainerOverlay(mountPoint, options) {
				mounts[mountPoint] = fsType
			}
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

func parseMountInfo(line string) (mountPoint, fsType, source, options string, ok bool) {
	parts := strings.Split(line, " - ")
	if len(parts) != 2 {
		return "", "", "", "", false
	}

	pre := strings.Fields(parts[0])
	post := strings.Fields(parts[1])
	if len(pre) < 5 || len(post) < 2 {
		return "", "", "", "", false
	}
	// The filesystem's own options, where an overlay names its folders.
	if len(post) > 2 {
		options = octalescape.Unescape(post[2])
	}

	mountPoint = filepath.Clean(octalescape.Unescape(pre[4]))
	fsType = strings.ToLower(post[0])
	source = strings.ToLower(post[1])
	return mountPoint, fsType, source, options, true
}
