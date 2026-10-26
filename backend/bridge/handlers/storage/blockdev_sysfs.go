package storage

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Where the inventory reads the host from; vars so tests can point them at a
// fake tree. sysClassBlockRoot is declared beside the free-space code.
var (
	udevDataRoot  = "/run/udev/data"
	procMountinfo = "/proc/self/mountinfo"
	procSwaps     = "/proc/swaps"
	devRoot       = "/dev"
)

// readSysString reads /sys/class/block/<kname>/<file> trimmed; "" when absent.
func readSysString(kname, file string) string {
	b, err := os.ReadFile(filepath.Join(sysClassBlockRoot, kname, file))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// sysEntries lists the entry names of /sys/class/block/<kname>/<dir>.
func sysEntries(kname, dir string) []string {
	entries, err := os.ReadDir(filepath.Join(sysClassBlockRoot, kname, dir))
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// blockPath is the node path of a kernel name: device-mapper devices are
// named under /dev/mapper, everything else sits directly in /dev.
func blockPath(kname string) string {
	if name := readSysString(kname, "dm/name"); name != "" {
		return "/dev/mapper/" + name
	}
	return "/dev/" + kname
}

// readUdev returns the E: properties udev recorded for a device number. A
// missing file (device not probed yet) is normal and yields no properties.
func readUdev(devno string) map[string]string {
	props := map[string]string{}
	f, err := os.Open(filepath.Join(udevDataRoot, "b"+devno))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Debug("read udev data failed", "device", devno, "error", err)
		}
		return props
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if kv, ok := strings.CutPrefix(scanner.Text(), "E:"); ok {
			if key, value, ok := strings.Cut(kv, "="); ok {
				props[key] = value
			}
		}
	}
	if err := scanner.Err(); err != nil {
		slog.Debug("read udev data failed", "device", devno, "error", err)
	}
	return props
}

// firstProp is the first non-empty of keys in props.
func firstProp(props map[string]string, keys ...string) string {
	for _, key := range keys {
		if v := props[key]; v != "" {
			return v
		}
	}
	return ""
}

// decodeUdevEnc undoes the \xNN escapes of udev's *_ENC properties.
func decodeUdevEnc(s string) string {
	if !strings.Contains(s, `\x`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && s[i+1] == 'x' {
			if n, err := strconv.ParseUint(s[i+2:i+4], 16, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// unescapeOctal undoes the \NNN escapes mountinfo and swaps use for spaces.
func unescapeOctal(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

type mountEntry struct {
	devno, mountpoint, source string
}

// readMountinfo lists the mounts of /proc/self/mountinfo. A source under /dev
// is already resolved to its canonical node.
func readMountinfo() ([]mountEntry, error) {
	f, err := os.Open(procMountinfo)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read mountinfo: %w", err)
	}
	defer f.Close()
	var mounts []mountEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// id parent major:minor root mountpoint options [optional...] - fstype source superopts
		left, right, ok := strings.Cut(scanner.Text(), " - ")
		fields, rest := strings.Fields(left), strings.Fields(right)
		if !ok || len(fields) < 5 || len(rest) < 2 {
			continue
		}
		mounts = append(mounts, mountEntry{
			devno: fields[2], mountpoint: unescapeOctal(fields[4]),
			source: resolveDevSource(unescapeOctal(rest[1])),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read mountinfo: %w", err)
	}
	return mounts, nil
}

// readSwaps lists the canonical sources of the active swap areas.
func readSwaps() ([]string, error) {
	f, err := os.Open(procSwaps)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read swaps: %w", err)
	}
	defer f.Close()
	var swaps []string
	scanner := bufio.NewScanner(f)
	scanner.Scan() // header
	for scanner.Scan() {
		if fields := strings.Fields(scanner.Text()); len(fields) > 0 {
			swaps = append(swaps, resolveDevSource(unescapeOctal(fields[0])))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read swaps: %w", err)
	}
	return swaps, nil
}

// resolveDevSource maps a /dev/... source (a by-id or mapper symlink, say) to
// the canonical /dev/<node>, resolving under devRoot. Anything it cannot
// resolve comes back unchanged to be compared as written.
func resolveDevSource(source string) string {
	rel, ok := strings.CutPrefix(source, "/dev/")
	if !ok {
		return source
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(devRoot, rel))
	if err != nil {
		return source
	}
	root, err := filepath.EvalSymlinks(devRoot)
	if err != nil {
		return source
	}
	rel, err = filepath.Rel(root, resolved)
	if err != nil || strings.HasPrefix(rel, "..") {
		return source
	}
	return "/dev/" + rel
}

// blockTransport follows lsblk's order: the SCSI host class first, then the
// device's own sysfs path, then the kernel name. "" when nothing matches.
func blockTransport(kname string) string {
	classRoot := filepath.Dir(sysClassBlockRoot)
	linkPath, _ := filepath.EvalSymlinks(filepath.Join(sysClassBlockRoot, kname, "device"))
	var host string
	for part := range strings.SplitSeq(linkPath, "/") {
		if n, ok := strings.CutPrefix(part, "host"); ok && n != "" && strings.Trim(n, "0123456789") == "" {
			host = part
		}
	}
	hostIs := func(class string) bool {
		if host == "" {
			return false
		}
		_, err := os.Stat(filepath.Join(classRoot, class+"_host", host))
		return err == nil
	}
	hasAttr := func(attr string) bool {
		_, err := os.Stat(filepath.Join(sysClassBlockRoot, kname, "device", attr))
		return err == nil
	}
	switch {
	case hostIs("spi"):
		return "spi"
	case hostIs("fcoe"):
		return "fcoe"
	case hostIs("fc"):
		return "fc"
	case hostIs("sas") || hasAttr("sas_device"):
		return "sas"
	case hasAttr("ieee1394_id"):
		return "sbp"
	case hostIs("iscsi"):
		return "iscsi"
	case strings.Contains(linkPath, "/usb"):
		return "usb"
	}
	if host != "" {
		b, _ := os.ReadFile(filepath.Join(classRoot, "scsi_host", host, "proc_name"))
		proc := strings.TrimSpace(string(b))
		switch {
		case proc == "ahci" || strings.HasPrefix(proc, "sata"):
			return "sata"
		case strings.Contains(proc, "ata"):
			return "ata"
		}
	}
	switch {
	case strings.HasPrefix(kname, "nvme"):
		return "nvme"
	case strings.HasPrefix(kname, "vd"):
		return "virtio"
	case strings.HasPrefix(kname, "mmcblk"):
		return "mmc"
	}
	return ""
}
