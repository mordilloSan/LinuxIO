package memory

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// fstabSwap is a swap entry of /etc/fstab: the node systemd swaps on, the
// unit systemd-fstab-generator makes for it, and the /proc/swaps path of the
// active device it is, if any.
type fstabSwap struct {
	path   string
	unit   string
	device string
}

// fstabTagDirs maps the fstab source tags to the udev link directories
// systemd-fstab-generator turns them into.
var fstabTagDirs = map[string]string{
	"UUID":      "/dev/disk/by-uuid",
	"LABEL":     "/dev/disk/by-label",
	"PARTUUID":  "/dev/disk/by-partuuid",
	"PARTLABEL": "/dev/disk/by-partlabel",
}

// fstabSwaps reads the swap entries of /etc/fstab, in order. An entry whose
// source systemd would not name a unit after is skipped: no unit can stop or
// start it.
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
		path, ok := fstabNodePath(unescapeOctal(fields[0]))
		if !ok {
			continue
		}
		unit, err := systemd.EscapePath(path)
		if err != nil {
			slog.Debug("skipped fstab swap entry", "source", fields[0], "error", err)
			continue
		}
		entries = append(entries, fstabSwap{path: filepath.Clean(path), unit: unit + ".swap"})
	}
	return entries, nil
}

// fstabNodePath is the path systemd-fstab-generator swaps on for an fstab
// source: a path as written, a tag through its /dev/disk/by-* link with "/"
// and blanks escaped as udev names them. Other sources name no unit.
func fstabNodePath(source string) (string, bool) {
	if strings.HasPrefix(source, "/") {
		return source, true
	}
	tag, value, ok := strings.Cut(source, "=")
	dir, known := fstabTagDirs[tag]
	if !ok || !known || value == "" {
		return "", false
	}
	value = strings.Trim(value, `"'`)
	var b strings.Builder
	for _, c := range []byte(value) {
		if c < ' ' || c >= 0x7f || c == '\\' || c == '/' || c == ' ' {
			fmt.Fprintf(&b, `\x%02x`, c)
		} else {
			b.WriteByte(c)
		}
	}
	return dir + "/" + b.String(), true
}

// maxSymlinks bounds the links resolve follows, as the kernel's 40 does.
const maxSymlinks = 40

// resolve follows every symbolic link in name the way the kernel would with
// the root as "/", an absolute link target included, and returns the path
// it reaches, which must exist.
func (h hostRoot) resolve(name string) (string, error) {
	pending := strings.Split(name, "/")
	resolved := "/"
	for links := 0; len(pending) > 0; {
		part := pending[0]
		pending = pending[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			resolved = filepath.Dir(resolved)
			continue
		}
		next := filepath.Join(resolved, part)
		info, err := os.Lstat(h.path(next))
		if err != nil {
			return "", err
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			resolved = next
			continue
		}
		if links++; links > maxSymlinks {
			return "", fmt.Errorf("resolve %s: too many levels of symbolic links", name)
		}
		target, err := os.Readlink(h.path(next))
		if err != nil {
			return "", err
		}
		if strings.HasPrefix(target, "/") {
			resolved = "/"
		}
		pending = append(strings.Split(target, "/"), pending...)
	}
	return resolved, nil
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

// matchFstabSwaps fills in the active device each fstab entry is. A device
// is matched to the first entry that is it.
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
	}
	return entries
}

// maskDirs are where systemctl mask links a unit to /dev/null: for good,
// and until the next boot.
var maskDirs = []string{"/etc/systemd/system", "/run/systemd/system"}

// unitMasked reports whether unit is masked on disk under the root, found
// without asking systemd: a link to /dev/null in one of maskDirs.
func (h hostRoot) unitMasked(unit string) bool {
	for _, dir := range maskDirs {
		target, err := os.Readlink(h.path(filepath.Join(dir, unit)))
		if err == nil && target == os.DevNull {
			return true
		}
	}
	return false
}

// managedDiskUsed is the data parked on the disk swap devices fstab names,
// the only ones a drain can stop and start again.
func managedDiskUsed(entries []fstabSwap, devices []apischema.SwapDevice) uint64 {
	var used uint64
	for _, device := range devices {
		for _, entry := range entries {
			if entry.device == device.Path {
				used += device.Used
				break
			}
		}
	}
	return used
}
