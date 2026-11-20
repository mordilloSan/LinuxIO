package btrfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/unix"
)

// A disk that drops out of a mounted pool, unplugged or off the bus, leaves
// the kernel holding a block device whose disk is gone. Since 7.1 the kernel
// marks that member missing itself (super.c btrfs_remove_bdev, also in 6.19
// and 7.0 built with CONFIG_BTRFS_EXPERIMENTAL), which markUnopened reads.
// Before, it keeps the btrfs_device with its dead block device until the
// pool is unmounted: DEV_INFO still names it by its path (ioctl.c
// btrfs_ioctl_dev_info), sysfs calls it neither missing nor unwriteable,
// chunks are still allocated on it, and every write to it fails. Nor does
// the pool take the disk back while mounted: a scan of the disk under
// another device number is refused as a duplicate, and one under the same
// path changes nothing (volumes.c device_list_add), so the dead block device
// stays until the pool is mounted again. Such a member counts as missing, so
// the episode opens while the pool runs on the others and the resync waits
// for the mount that takes the disk back.

// deviceNotBusy opens a device exclusively and closes it again, failing
// with EBUSY while something holds it; a var so tests can answer for it.
var deviceNotBusy = CheckNotBusy

// markDroppedOut marks Missing and DroppedOut each member whose block device
// went away while the pool is mounted, and reports whether it could tell for
// every member.
//
// The kernel links each block device it opened for the pool under
// /sys/fs/btrfs/<uuid>/devices, named after it (sysfs.c
// btrfs_sysfs_add_device, bdev_kobj), and removes the link only when the
// member leaves the pool. A block device that went away is deleted from
// sysfs, so its link reaches nothing. One whose name a device has again, the
// same disk back or another, is reached by the link but is not what the pool
// holds: btrfs holds its members exclusively (bdev_file_open_by_path with a
// holder), so an exclusive open fails with EBUSY on a member it holds and
// succeeds on any other device. A read-only open fires no udev change event,
// and a refused one opens nothing.
//
// DEV_INFO names a member by the path it was registered by: /dev/<name> as
// udev and btrfs-progs register it, else a link such as /dev/mapper/<name>,
// or one a mount was given, which 6.8 keeps. A member's link is the one of
// its own name, else of the device its path resolves to. A member no link
// matches, as /dev/root for a root the kernel mounted without an initramfs,
// counts as dropped out only when as many links of gone devices match no
// member, and no member is missing already: a member the kernel marked
// missing has its link too (since 7.1).
func markDroppedOut(uuid string, devices []BtrfsDevice, warn func(string, error)) bool {
	gone, known := goneLinks(filepath.Join(sysFSBtrfs, uuid, "devices"), warn)
	missingBefore := slices.ContainsFunc(devices, func(d BtrfsDevice) bool { return d.Missing })
	link, taken := matchLinks(devices, gone)
	var unmatched []int
	for i, d := range devices {
		n, linked := link[i]
		switch {
		case d.Missing:
		case !linked:
			unmatched = append(unmatched, i)
		case gone[n]:
			devices[i].Missing, devices[i].DroppedOut = true, true
		}
	}
	orphans := 0
	for n, g := range gone {
		if g && !taken[n] {
			orphans++
		}
	}
	switch {
	case missingBefore, len(unmatched) == 0, orphans == 0:
	case len(unmatched) == orphans:
		for _, i := range unmatched {
			devices[i].Missing, devices[i].DroppedOut = true, true
		}
	default:
		warn("device holder", fmt.Errorf("%d members of pool %s match no device link, and %d links of gone devices match no member", len(unmatched), uuid, orphans))
		known = false
	}
	return known
}

// goneLinks reads the kernel's links to the pool's block devices in dir and
// whether each one's device went away. No list (a kernel too old for it)
// gives none, and every member stays as listed. The bool is false when a
// link could not be told.
func goneLinks(dir string, warn func(string, error)) (map[string]bool, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, true
	}
	known := true
	gone := map[string]bool{}
	for _, e := range entries {
		g, err := linkGone(filepath.Join(dir, e.Name()), "/dev/"+strings.ReplaceAll(e.Name(), "!", "/"))
		if err != nil {
			warn("device holder", err)
			known = false
		}
		gone[e.Name()] = g
	}
	return gone, known
}

// matchLinks matches each member not missing to a link of gone, by its own
// name first, then by the device its path resolves to, and returns the
// matches and the links taken.
func matchLinks(devices []BtrfsDevice, gone map[string]bool) (map[int]string, map[string]bool) {
	link := map[int]string{}
	taken := map[string]bool{}
	for _, name := range []func(string) string{func(p string) string { return p }, canonicalPath} {
		for i, d := range devices {
			if _, linked := link[i]; d.Missing || linked {
				continue
			}
			n := sysfsName(name(d.Path))
			if _, ok := gone[n]; ok && !taken[n] {
				link[i], taken[n] = n, true
			}
		}
	}
	return link, taken
}

// sysfsName is the name sysfs gives the block device at path under /dev,
// with a "/" written as "!" (cciss!c0d0), or "" for a path outside /dev.
func sysfsName(path string) string {
	name, ok := strings.CutPrefix(path, "/dev/")
	if !ok {
		return ""
	}
	return strings.ReplaceAll(name, "/", "!")
}

// linkGone reports whether the block device the kernel linked at link went
// away: the link reaches nothing, or node, the device by that name now, is a
// device nothing holds or has no device behind it. The pool removes a link
// before it lets its device go, as a member leaves it or at unmount
// (volumes.c btrfs_rm_device, disk-io.c close_ctree), so a device found
// gone counts only while its link is still there. A node /dev does not have
// while sysfs lists the device, and a reader not allowed to open it (a
// bridge of a session without sudo), read as not gone: such a reader still
// sees a link that reaches nothing, and calling every pool unknown for it
// would hide more than it tells.
func linkGone(link, node string) (bool, error) {
	if _, err := os.Stat(link); errors.Is(err, fs.ErrNotExist) {
		return linked(link), nil
	} else if err != nil {
		return false, err
	}
	err := deviceNotBusy(node)
	switch {
	case err == nil, errors.Is(err, unix.ENXIO), errors.Is(err, unix.ENODEV):
		return linked(link), nil
	case errors.Is(err, unix.EBUSY), errors.Is(err, fs.ErrNotExist), errors.Is(err, fs.ErrPermission):
		return false, nil
	}
	return false, err
}

// linked reports whether the link itself is still there.
func linked(link string) bool {
	_, err := os.Lstat(link)
	return err == nil
}
