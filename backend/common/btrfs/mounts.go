package btrfs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/octalescape"
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
// A mount's filesystem is told by its source, a member of it; when the source
// names no member, as when the kernel shows it as "<missing disk>" (since
// 7.1, once that member's disk is gone: btrfs_show_devname), the mount itself
// is asked (mountedUUID).
func ListMounts(context.Context) ([]Mount, error) {
	data, err := os.ReadFile(mountinfoPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", mountinfoPath, err)
	}
	byDevice := deviceUUIDs()
	return parseMountinfo(string(data), func(id uint64, mountpoint, device string) string {
		// Only a path names a device: "<missing disk>" would be looked up,
		// and read, as a file of that name in the working directory.
		if filepath.IsAbs(device) {
			if uuid := byDevice(device); uuid != "" {
				return uuid
			}
		}
		uuid, err := mountedUUID(mountpoint, id)
		if err != nil {
			slog.Debug("btrfs mount names no member and its filesystem could not be read", "mountpoint", mountpoint, "device", device, "error", err)
		}
		return uuid
	}), nil
}

// openMountpoint opens a mountpoint as mountinfo lists it, which is a path
// without links, so the open follows none: a folder of the path replaced by
// a link since is refused. A var so tests can fake it.
var openMountpoint = func(mountpoint string) (int, error) {
	fd, err := unix.Openat2(unix.AT_FDCWD, mountpoint, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return -1, fmt.Errorf("open %s: %w", mountpoint, err)
	}
	return fd, nil
}

// mountedUUID returns the UUID of the filesystem of mount id, mounted at
// mountpoint, by FS_INFO through an open of the mountpoint that must reach
// that mount (its mount ID by statx): not one mounted over it since, nor
// another folder a renamed parent now leads to; a var so tests can fake it.
var mountedUUID = func(mountpoint string, id uint64) (string, error) {
	fd, err := openMountpoint(mountpoint)
	if err != nil {
		return "", err
	}
	defer unix.Close(fd)
	var stx unix.Statx_t
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &stx); err != nil {
		return "", fmt.Errorf("statx %s: %w", mountpoint, err)
	}
	if stx.Mask&unix.STATX_MNT_ID == 0 || stx.Mnt_id != id {
		return "", fmt.Errorf("%s opens mount %d, not mount %d", mountpoint, stx.Mnt_id, id)
	}
	var info fsInfoArgs
	if err := ioctlFD(fd, iocFSInfo, unsafe.Pointer(&info)); err != nil {
		return "", fmt.Errorf("fs info of %s: %w", mountpoint, err)
	}
	return formatUUID(info.FSID[:]), nil
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
// superopts", with spaces in paths escaped as \040. uuidOf names each
// mount's filesystem from its mount ID, mountpoint and source.
func parseMountinfo(text string, uuidOf func(id uint64, mountpoint, device string) string) []Mount {
	mounts := []Mount{}
	for line := range strings.Lines(text) {
		fields := strings.Fields(line)
		sep := slices.Index(fields, "-")
		if sep < 6 || len(fields) < sep+4 || fields[sep+1] != "btrfs" {
			continue
		}
		id, _ := strconv.ParseUint(fields[0], 10, 64)
		mountpoint, device := octalescape.Unescape(fields[4]), octalescape.Unescape(fields[sep+2])
		m := Mount{
			Mountpoint: mountpoint, Device: device, UUID: uuidOf(id, mountpoint, device),
			SubPath: strings.TrimPrefix(octalescape.Unescape(fields[3]), "/"),
			Options: append(strings.Split(fields[5], ","), strings.Split(fields[sep+3], ",")...),
		}
		for _, opt := range m.Options {
			switch key, val, _ := strings.Cut(opt, "="); key {
			case "subvol":
				// Escaped like the root field, which SubPath is compared with.
				m.Subvol = strings.TrimPrefix(octalescape.Unescape(val), "/")
			case "subvolid":
				m.SubvolID, _ = strconv.Atoi(val)
			}
		}
		mounts = append(mounts, m)
	}
	return mounts
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

// deviceOf returns a member device of the mounted filesystem, to mount(2) it
// by: the source of a mount of it while that block device exists, or else a
// member the pool still holds, asked of each mount in turn: since 7.1 the
// kernel shows a gone source as "<missing disk>".
func deviceOf(mounts []Mount, uuid string) (string, error) {
	var errs []error
	for _, m := range mounts {
		if m.UUID == uuid && filepath.IsAbs(m.Device) && blockDeviceExists(m.Device) {
			return m.Device, nil
		}
	}
	for _, m := range mounts {
		if m.UUID != uuid {
			continue
		}
		device, err := heldMember(m.Mountpoint, uuid)
		if err == nil {
			return device, nil
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return "", fmt.Errorf("btrfs filesystem %s is not mounted", uuid)
	}
	return "", errors.Join(errs...)
}

// blockDeviceExists says whether path is a block device now: before 7.1 the
// kernel keeps naming a member whose disk dropped out by its old path, in
// mountinfo and DEV_INFO alike, and a mount by it would fail after a
// rollback's remount had already unmounted. A var so tests can fake it.
var blockDeviceExists = func(path string) bool {
	var st unix.Stat_t
	return unix.Stat(path, &st) == nil && st.Mode&unix.S_IFMT == unix.S_IFBLK
}

// heldMember returns the path DEV_INFO gives a member of pool uuid that is
// not missing, the kernel's name for a block device it holds. It asks
// through one open of mnt, which must still be a mount of that pool: the
// list of mounts was read earlier, and another filesystem's member would
// get that filesystem mounted in the pool's place.
func heldMember(mnt, uuid string) (string, error) {
	fd, err := openMountpoint(mnt)
	if err != nil {
		return "", err
	}
	defer unix.Close(fd)
	var info fsInfoArgs
	if err := ioctlFD(fd, iocFSInfo, unsafe.Pointer(&info)); err != nil {
		return "", fmt.Errorf("fs info of %s: %w", mnt, err)
	}
	if got := formatUUID(info.FSID[:]); got != uuid {
		return "", fmt.Errorf("%s is a mount of btrfs filesystem %s, no longer of %s", mnt, got, uuid)
	}
	for id := uint64(1); id <= info.MaxID; id++ {
		dev := devInfoArgs{DevID: id}
		if err := ioctlFD(fd, iocDevInfo, unsafe.Pointer(&dev)); err != nil {
			if errors.Is(err, unix.ENODEV) {
				continue
			}
			return "", fmt.Errorf("device info %d of %s: %w", id, mnt, err)
		}
		if path := cstring(dev.Path[:]); filepath.IsAbs(path) && blockDeviceExists(path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("btrfs filesystem %s has no member present to mount it by", uuid)
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
		// A bind mount of a folder of the top level has its subvolume
		// too, but a path from the top level taken from it would name
		// another folder.
		if m.UUID == uuid && m.SubvolID == topLevelSubvolID && m.SubPath == "" {
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
