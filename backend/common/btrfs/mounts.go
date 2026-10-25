package btrfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// TopLevelDir is where TopLevel mounts filesystems that have no top-level mount.
var TopLevelDir = version.RunDir + "/btrfs"

// topLevelSubvolID is the id of the btrfs top-level subvolume.
const topLevelSubvolID = 5

// Mount is one mounted btrfs subvolume.
type Mount struct {
	Mountpoint string   `json:"mountpoint"`
	Device     string   `json:"device"`
	UUID       string   `json:"uuid"`
	Subvol     string   `json:"subvol"` // path without leading slash; "" for the top level
	SubvolID   int      `json:"subvolId"`
	Options    []string `json:"options"`
	// SubPath is the filesystem path the mount shows, from mountinfo's root
	// field without the leading slash: Subvol for a subvolume mount, a path
	// inside it (@/www) for a bind mount, "" for the top level.
	SubPath string `json:"subPath,omitempty"`
}

// listMounts is a variable so tests can stub mount discovery.
var listMounts = ListMounts

// mountinfoPath and sysFSBtrfs are vars so tests can point them at fixtures.
var (
	mountinfoPath = "/proc/self/mountinfo"
	sysFSBtrfs    = "/sys/fs/btrfs"
)

// unmountFS is unix.Unmount, a var so tests can stub it.
var unmountFS = unix.Unmount

// ListMounts returns the mounted btrfs filesystems from /proc/self/mountinfo.
func ListMounts(context.Context) ([]Mount, error) {
	data, err := os.ReadFile(mountinfoPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", mountinfoPath, err)
	}
	return parseMountinfo(string(data), deviceUUIDs()), nil
}

// evalSymlinks is a variable so tests can stub path canonicalisation.
var evalSymlinks = filepath.EvalSymlinks

func canonicalPath(p string) string {
	if resolved, err := evalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

// deviceUUIDs maps devices to filesystem UUIDs from the kernel's own list
// of every mounted filesystem's members, /sys/fs/btrfs/<uuid>/devices/<name>,
// which needs no privilege; a device not listed there has its superblock read.
func deviceUUIDs() func(device string) string {
	byName := map[string]string{}
	filesystems, _ := os.ReadDir(sysFSBtrfs)
	for _, fs := range filesystems {
		members, err := os.ReadDir(filepath.Join(sysFSBtrfs, fs.Name(), "devices"))
		if err != nil {
			continue // features/ and other non-filesystem entries
		}
		for _, m := range members {
			byName[m.Name()] = fs.Name()
		}
	}
	return func(device string) string {
		if uuid, ok := byName[filepath.Base(canonicalPath(device))]; ok {
			return uuid
		}
		if sb, err := readSuperblock(device); err == nil {
			return sb.UUID
		}
		return ""
	}
}

// parseMountinfo keeps the btrfs lines of mountinfo. Each line is
// "id parent maj:min root mountpoint vfsopts [optional...] - fstype source
// superopts", with spaces in paths escaped as \040.
func parseMountinfo(text string, uuidOf func(string) string) []Mount {
	mounts := []Mount{}
	for line := range strings.Lines(text) {
		fields := strings.Fields(line)
		sep := slices.Index(fields, "-")
		if sep < 6 || len(fields) < sep+4 || fields[sep+1] != "btrfs" {
			continue
		}
		device := unescapeMountinfo(fields[sep+2])
		m := Mount{
			Mountpoint: unescapeMountinfo(fields[4]), Device: device, UUID: uuidOf(device),
			SubPath: strings.TrimPrefix(unescapeMountinfo(fields[3]), "/"),
			Options: append(strings.Split(fields[5], ","), strings.Split(fields[sep+3], ",")...),
		}
		for _, opt := range m.Options {
			switch key, val, _ := strings.Cut(opt, "="); key {
			case "subvol":
				m.Subvol = strings.TrimPrefix(val, "/")
			case "subvolid":
				m.SubvolID, _ = strconv.Atoi(val)
			}
		}
		mounts = append(mounts, m)
	}
	return mounts
}

// unescapeMountinfo decodes the \ooo octal escapes mountinfo uses.
func unescapeMountinfo(s string) string {
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

// vfsMountFlags are the options mount(2) takes as flags rather than in the
// data string the filesystem parses.
var vfsMountFlags = map[string]uintptr{
	"ro": unix.MS_RDONLY, "nosuid": unix.MS_NOSUID, "nodev": unix.MS_NODEV, "noexec": unix.MS_NOEXEC,
	"sync": unix.MS_SYNCHRONOUS, "dirsync": unix.MS_DIRSYNC, "noatime": unix.MS_NOATIME, "nodiratime": unix.MS_NODIRATIME,
	"relatime": unix.MS_RELATIME, "strictatime": unix.MS_STRICTATIME, "lazytime": unix.MS_LAZYTIME, "mand": unix.MS_MANDLOCK,
}

// kernelMountOptions drops the fstab options that mount(8) and systemd act
// on themselves, and the ones naming a default mount(2) already applies,
// which btrfs would reject.
func kernelMountOptions(options []string) []string {
	return slices.DeleteFunc(slices.Clone(options), func(o string) bool {
		switch o {
		case "auto", "noauto", "nofail", "user", "users", "nouser", "owner", "group", "_netdev",
			"async", "exec", "suid", "dev", "atime", "diratime", "norelatime", "nostrictatime", "nolazytime", "iversion", "noiversion", "loud":
			return true
		}
		return strings.HasPrefix(strings.ToLower(o), "x-") || strings.HasPrefix(o, "comment=")
	})
}

// splitMountOptions separates mount flags from filesystem options; "rw" and
// "defaults" add nothing.
func splitMountOptions(options []string) (flags uintptr, data string) {
	var fsOptions []string
	for _, o := range options {
		if f, ok := vfsMountFlags[o]; ok {
			flags |= f
		} else if o != "" && o != "rw" && o != "defaults" {
			fsOptions = append(fsOptions, o)
		}
	}
	return flags, strings.Join(fsOptions, ",")
}

// deviceOf returns a member device of the mounted filesystem.
func deviceOf(mounts []Mount, uuid string) (string, error) {
	for _, m := range mounts {
		if m.UUID == uuid && m.Device != "" {
			return m.Device, nil
		}
	}
	return "", fmt.Errorf("btrfs filesystem %s is not mounted", uuid)
}

// TopLevel returns a mountpoint of the filesystem's top-level subvolume,
// mounting it under TopLevelDir when no such mount exists.
func TopLevel(ctx context.Context, uuid string) (string, error) {
	if uuid == "" || !filepath.IsLocal(uuid) || strings.ContainsRune(uuid, filepath.Separator) {
		return "", fmt.Errorf("invalid filesystem uuid %q", uuid)
	}
	mounts, err := listMounts(ctx)
	if err != nil {
		return "", err
	}
	for _, m := range mounts {
		if m.UUID == uuid && m.SubvolID == topLevelSubvolID {
			return m.Mountpoint, nil
		}
	}
	device, err := deviceOf(mounts, uuid)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(TopLevelDir, uuid)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create top-level mount dir: %w", err)
	}
	if err := mountFS(device, dir, "btrfs", unix.MS_NOATIME, "subvolid=5"); err != nil {
		return "", fmt.Errorf("mount top level of %s: %w", uuid, err)
	}
	return dir, nil
}
