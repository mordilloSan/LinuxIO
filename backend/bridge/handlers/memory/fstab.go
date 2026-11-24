package memory

import (
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/octalescape"
)

// fstabSwap is a swap file /etc/fstab names by a plain path: the path as the
// first field gives it after \ooo unescaping, and the options field of the
// first line that names it, which swapOn turns into swapon's options.
type fstabSwap struct {
	path    string
	options string
}

// fstabSwaps reads the swap files of /etc/fstab, in the order their first
// line comes: every line whose type is swap and whose first field, unescaped,
// is an absolute path outside /dev. A tag such as UUID= and a device under
// /dev are left out: LinuxIO never turns those off. A missing fstab has none.
func (h hostRoot) fstabSwaps() ([]fstabSwap, error) {
	text, _, err := h.readOptional(etcFstab)
	if err != nil {
		return nil, err
	}
	var entries []fstabSwap
	for line := range strings.Lines(text) {
		fields := strings.Fields(line)
		if len(fields) < 3 || strings.HasPrefix(fields[0], "#") || fields[2] != "swap" {
			continue
		}
		path := octalescape.Unescape(fields[0])
		if !isSwapFilePath(path) {
			continue
		}
		if fstabIndex(entries, path) >= 0 {
			continue
		}
		entry := fstabSwap{path: path}
		if len(fields) > 3 {
			entry.options = fields[3]
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// fstabIndex is the index of the entry for path, or -1.
func fstabIndex(entries []fstabSwap, path string) int {
	for i, entry := range entries {
		if entry.path == path {
			return i
		}
	}
	return -1
}

// isSwapFilePath reports whether an fstab swap source names a file rather
// than a tag or a block device: an absolute path outside /dev.
func isSwapFilePath(path string) bool {
	return strings.HasPrefix(path, "/") && path != "/dev" && !strings.HasPrefix(path, "/dev/")
}

// fstabNames reports whether /etc/fstab names path as a swap file. A file
// that cannot be read counts as naming it not.
func (h hostRoot) fstabNames(path string) bool {
	entries, err := h.fstabSwaps()
	return err == nil && fstabIndex(entries, path) >= 0
}
