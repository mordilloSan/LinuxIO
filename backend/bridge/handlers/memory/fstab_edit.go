package memory

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/octalescape"
)

// fstabEditLock is the lock every read-modify-write of /etc/fstab by
// LinuxIO takes, the storage pages' and the btrfs health run's among them
// (btrfs.FstabLockPath); a variable so that a test uses a lock of its own.
var fstabEditLock = btrfs.FstabLockPath

// fstabEditTimeout bounds the wait for the fstab lock, as the storage pages
// bound theirs.
const fstabEditTimeout = 10 * time.Second

// fstabSwapOptions are the options of the line addFstabSwap writes: nofail
// makes swap.target want the file's unit instead of requiring it, so that a
// file missing at boot keeps nothing else from starting.
const fstabSwapOptions = "sw,nofail"

// addFstabSwap appends the line "<path> none swap sw,nofail 0 0" to
// /etc/fstab, under the fstab lock the storage pages take and written
// atomically, unless a swap line already names path. Every other byte of the
// file stays as it was; a last line without its newline gets one before the
// new line. A path fstab would need escaped is refused: only ownSwapFilePath
// is ever added.
func (h hostRoot) addFstabSwap(ctx context.Context, path string) error {
	if err := checkSwapFilePath(path); err != nil {
		return err
	}
	if strings.ContainsFunc(path, func(r rune) bool { return r == '\\' || unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return fmt.Errorf("%w: %q would need escaping in /etc/fstab", bridgeipc.ErrInvalidArgs, path)
	}
	return h.editFstab(ctx, func(content string) (string, bool) {
		for line := range strings.Lines(content) {
			if fstabLineSwaps(line, path) {
				return content, false
			}
		}
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		return content + path + " none swap " + fstabSwapOptions + " 0 0\n", true
	})
}

// removeFstabSwap removes from /etc/fstab, under the same lock and written
// atomically, every line whose type is swap and whose first field, unescaped,
// is exactly path. Every other byte of the file stays as it was. No such
// line is not an error, and neither is a missing /etc/fstab.
func (h hostRoot) removeFstabSwap(ctx context.Context, path string) error {
	if err := checkSwapFilePath(path); err != nil {
		return err
	}
	return h.editFstab(ctx, func(content string) (string, bool) {
		var kept strings.Builder
		changed := false
		for line := range strings.Lines(content) {
			if fstabLineSwaps(line, path) {
				changed = true
				continue
			}
			kept.WriteString(line)
		}
		return kept.String(), changed
	})
}

// fstabLineSwaps reports whether line, with or without its newline, is an
// entry of type swap whose first field, unescaped, is path. A comment is
// not an entry, whatever it holds.
func fstabLineSwaps(line, path string) bool {
	fields := strings.Fields(line)
	if len(fields) < 3 || strings.HasPrefix(fields[0], "#") {
		return false
	}
	return fields[2] == "swap" && octalescape.Unescape(fields[0]) == path
}

// editFstab rewrites /etc/fstab with what edit returns for its content, when
// edit says it changed, under the fstab lock. A missing file reads as empty.
func (h hostRoot) editFstab(ctx context.Context, edit func(content string) (string, bool)) error {
	name := h.path(etcFstab)
	return filelock.RunExclusive(ctx, fstabEditLock, func() error {
		data, err := os.ReadFile(name)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		content, changed := edit(string(data))
		if !changed {
			return nil
		}
		return atomicfile.Write(name, []byte(content), 0o644)
	}, filelock.WithTimeout(fstabEditTimeout))
}
