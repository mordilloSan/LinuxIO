package memory

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/octalescape"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// fstabSwap is a swap entry of /etc/fstab: the node systemd swaps on, the
// unit systemd-fstab-generator makes for it, the /proc/swaps path of the
// active device it is, if any, and whether its unit is held off at boot
// (see unitHeldOff).
// othersCanWrite is set on an entry that someone other than root can put
// another entry at the path of, which LinuxIO does not manage.
type fstabSwap struct {
	path           string
	unit           string
	device         string
	heldOff        bool
	othersCanWrite bool
}

// fstabTagDirs maps the fstab source tags to the udev link directories
// systemd-fstab-generator turns them into.
var fstabTagDirs = map[string]string{
	"UUID":      "/dev/disk/by-uuid",
	"LABEL":     "/dev/disk/by-label",
	"PARTUUID":  "/dev/disk/by-partuuid",
	"PARTLABEL": "/dev/disk/by-partlabel",
}

// unusableFstabSwap is a swap entry of /etc/fstab without a systemd unit
// LinuxIO can use (see fstabSwapUnit), so no step stops, starts, holds off
// or drains it: its source as fstab writes it, and the path it swaps on when
// the source is a path.
type unusableFstabSwap struct {
	source, path string
}

// fstabSwaps reads the swap entries of /etc/fstab, in order: those whose
// unit LinuxIO names as systemd-fstab-generator does, and apart from them
// those without a unit it can use, which it leaves alone.
func (h hostRoot) fstabSwaps() ([]fstabSwap, []unusableFstabSwap, error) {
	text, _, err := h.readOptional(etcFstab)
	if err != nil {
		return nil, nil, err
	}
	var entries []fstabSwap
	var unusable []unusableFstabSwap
	for line := range strings.Lines(text) {
		fields := strings.Fields(line)
		if len(fields) < 3 || strings.HasPrefix(fields[0], "#") || fields[2] != "swap" {
			continue
		}
		source := octalescape.Unescape(fields[0])
		path, unit, err := fstabSwapUnit(source)
		if err != nil {
			slog.Debug("skipped fstab swap entry", "source", fields[0], "error", err)
			entry := unusableFstabSwap{source: fields[0]}
			if strings.HasPrefix(source, "/") {
				entry.path = source
			}
			unusable = append(unusable, entry)
			continue
		}
		entries = append(entries, fstabSwap{path: filepath.Clean(path), unit: unit})
	}
	return entries, unusable, nil
}

const (
	// unitNameMax is systemd's UNIT_NAME_MAX: a unit named after a path that
	// would be this long or longer gets a name shortened with a hash of the
	// path instead (unit_name_from_path).
	unitNameMax = 256
	// fileNameMax is the longest file name Linux takes (NAME_MAX), which the
	// unit's drop-in folder, "<unit>.d", must fit.
	fileNameMax = 255
)

// fstabSwapUnit is the node systemd-fstab-generator swaps on for an fstab
// source and the swap unit it names after that node. It fails for a unit
// LinuxIO cannot use: one it cannot name as systemd does (a source
// fstabNodePath refuses, a path systemd-escape refuses, a unit name systemd
// shortens with a hash), or one whose drop-in folder, which holds the unit
// off at boot, would be longer than a file name may be.
func fstabSwapUnit(source string) (path, unit string, err error) {
	if path, err = fstabNodePath(source); err != nil {
		return "", "", err
	}
	name, err := systemd.EscapePath(path)
	if err != nil {
		return "", "", err
	}
	unit = name + ".swap"
	switch {
	case len(unit) >= unitNameMax:
		return "", "", fmt.Errorf("systemd names the unit of %s with a hash, its name being %d bytes", path, len(unit))
	case len(unit+".d") > fileNameMax:
		return "", "", fmt.Errorf("the drop-in folder of %s would be %d bytes, longer than a file name may be", unit, len(unit+".d"))
	}
	return path, unit, nil
}

// fstabNodePath is the path systemd-fstab-generator swaps on for an fstab
// source: a path as written, or a tag through its /dev/disk/by-* link. The
// tag's value loses one pair of quotes around it, as systemd's unquote
// takes off, and is escaped as udev's encode_devnode_name does: letters,
// digits and #+-.:=@_ as they are, "\", "/", blanks and control characters
// as \xNN. Any other byte, punctuation udev escapes or a character outside
// ASCII, which udev keeps as it is when it is valid UTF-8, is refused rather
// than named wrong, as is any other source.
func fstabNodePath(source string) (string, error) {
	if strings.HasPrefix(source, "/") {
		return source, nil
	}
	tag, value, ok := strings.Cut(source, "=")
	dir, known := fstabTagDirs[tag]
	if !ok || !known || value == "" {
		return "", errors.New("the source is neither a path nor a tag LinuxIO knows")
	}
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	}
	var b strings.Builder
	for _, c := range []byte(value) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', strings.IndexByte("#+-.:=@_", c) >= 0:
			b.WriteByte(c)
		case c <= ' ' || c == 0x7f || c == '\\' || c == '/':
			fmt.Fprintf(&b, `\x%02x`, c)
		default:
			return "", fmt.Errorf("udev may write the byte %#x of the %s value otherwise in its link", c, tag)
		}
	}
	return dir + "/" + b.String(), nil
}

// maxSymlinks bounds the links resolve follows, as the kernel's 40 does.
const maxSymlinks = 40

// resolve follows every symbolic link in name the way the kernel would with
// the root as "/", an absolute link target included, and returns the path
// it reaches, which must exist.
func (h hostRoot) resolve(name string) (string, error) {
	resolved, _, err := h.resolveLinks(name)
	return resolved, err
}

// resolveLinks is resolve that also returns, in order and including those
// before it failed, the path of every link it followed and of every folder
// a link target left by "..": what the path leads to depends on each.
func (h hostRoot) resolveLinks(name string) (string, []string, error) {
	var followed []string
	pending := strings.Split(name, "/")
	resolved := "/"
	for links := 0; len(pending) > 0; {
		part := pending[0]
		pending = pending[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			followed = append(followed, resolved)
			resolved = filepath.Dir(resolved)
			continue
		}
		next := filepath.Join(resolved, part)
		info, err := os.Lstat(h.path(next))
		if err != nil {
			return "", followed, err
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			resolved = next
			continue
		}
		if links++; links > maxSymlinks {
			return "", followed, fmt.Errorf("resolve %s: too many levels of symbolic links", name)
		}
		followed = append(followed, next)
		target, err := os.Readlink(h.path(next))
		if err != nil {
			return "", followed, err
		}
		if strings.HasPrefix(target, "/") {
			resolved = "/"
		}
		pending = append(strings.Split(target, "/"), pending...)
	}
	return resolved, followed, nil
}

// sameSwap reports whether an fstab path is the /proc/swaps device: by its
// literal name, or because both resolve to the same path under the root.
func (h hostRoot) sameSwap(fstabPath, device string) bool {
	if filepath.Clean(fstabPath) == device {
		return true
	}
	a, err := h.resolve(fstabPath)
	if err != nil {
		return false
	}
	b, err := h.resolve(device)
	return err == nil && a == b
}

// matchFstabSwaps fills in the active device each fstab entry is, whether
// its unit is held off at boot, and then whether others can change the
// entry's path. A
// device is matched to the first entry that is it.
func (h hostRoot) matchFstabSwaps(entries []fstabSwap, devices []apischema.SwapDevice) []fstabSwap {
	taken := map[string]bool{}
	for i := range entries {
		for _, device := range devices {
			if device.Kind == apischema.SwapDeviceZram || taken[device.Path] || !h.sameSwap(entries[i].path, device.Path) {
				continue
			}
			entries[i].device, taken[device.Path] = device.Path, true
			break
		}
		entries[i].heldOff = h.unitHeldOff(entries[i].unit)
		entries[i].othersCanWrite = h.othersCanWrite(entries[i])
	}
	return entries
}

// othersCanWrite reports whether someone other than the root's owner can put
// another entry where an fstab swap entry is looked up: on the path fstab
// names, which systemd swaps on for a start; at any link followed from it,
// or at a folder a link target leaves by "..", either of which could be
// pointed elsewhere after this check whatever it names now; on the path
// they lead to; or on the /proc/swaps path swapoff is given while the entry
// is active. LinuxIO does not manage such an entry. Links
// that lead nowhere cannot be judged and count. A folder the reader may not
// look into is not judged: a session without sudo reads the status too, and
// root, which makes the changes, can look into every folder.
func (h hostRoot) othersCanWrite(entry fstabSwap) bool {
	real, links, err := h.resolveLinks(entry.path)
	paths := append([]string{entry.path}, links...)
	switch {
	case err == nil:
		paths = append(paths, real)
	case errors.Is(err, fs.ErrPermission):
	case len(links) > 0:
		return true
	}
	if entry.device != "" {
		paths = append(paths, entry.device)
	}
	for _, path := range paths {
		if err := foldersOnlyOf(h.owner, h.root, h.path(path)); err != nil && !errors.Is(err, fs.ErrPermission) {
			return true
		}
	}
	return false
}

// maskDirs are where systemctl mask links a unit to /dev/null: for good,
// and until the next boot.
var maskDirs = []string{"/etc/systemd/system", "/run/systemd/system"}

// unitHeldOff reports whether unit is held off at boot, found on disk under
// the root without asking systemd: LinuxIO's drop-in for it is there
// (holdOffDropInPath), or it is masked, a link to /dev/null in one of
// maskDirs, as an admin, or a LinuxIO that held swap files off with a mask,
// leaves it.
func (h hostRoot) unitHeldOff(unit string) bool {
	if h.exists(holdOffDropInPath(unit)) {
		return true
	}
	for _, dir := range maskDirs {
		target, err := os.Readlink(h.path(filepath.Join(dir, unit)))
		if err == nil && target == os.DevNull {
			return true
		}
	}
	return false
}

// managedDiskUsed is the data parked on the disk swap devices LinuxIO
// manages, the only ones a drain stops and starts again: those fstab names,
// less one others can redirect.
func managedDiskUsed(entries []fstabSwap, devices []apischema.SwapDevice) uint64 {
	var used uint64
	for _, device := range devices {
		for _, entry := range entries {
			if entry.device == device.Path && !entry.othersCanWrite {
				used += device.Used
				break
			}
		}
	}
	return used
}
