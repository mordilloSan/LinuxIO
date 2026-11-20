package btrfs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// A complete pool can stay unmounted after a boot because udev leaves it
// unready. 64-btrfs.rules asks the kernel whether a member's filesystem is
// complete (builtin "btrfs ready", BTRFS_IOC_DEVICES_READY) and sets
// SYSTEMD_READY=0 on a member scanned before the others; the member that
// completes the pool re-triggers those with `udevadm trigger -s block -p
// ID_BTRFS_READY=0`, which matches on the udev database. When two members
// are scanned at once, that trigger can miss the first, which then stays
// unready. Every member claims /dev/disk/by-uuid/<uuid> at the same priority
// and the one processed last owns it (udev-node.c link_update), and systemd
// decides on the device the link points to (device.c
// device_setup_devlink_unit_one): when that is the unready member, the device
// unit never starts and the fstab mount gives up after its device timeout.
// The kernel sends no uevent when a filesystem becomes complete. So the boot
// check does what the rule's trigger is for: it writes "change" to the
// uevent of each member udev left with ID_BTRFS_READY=0, udev runs its rules
// again, and systemd mounts the pool itself, with its own options and
// ordering.
//
// It acts on the members the rule itself scanned and on nothing else: udev's
// database is the record of that (/run/udev/data, ID_FS_TYPE=btrfs and the
// ID_BTRFS_READY the rule's builtin sets), and a device something holds is
// left out. A leg of an md array with its metadata at the end, a path of a
// multipath device or an image of an LVM mirror shows the btrfs superblock
// too, but udev probes it as what it is a part of, and registering it with
// the kernel would let btrfs open the leg under the array.

// udevSettle is how long the boot check gives systemd to find the device of
// a complete pool before it has udev evaluate the members again. Once udev
// has scanned the last member, systemd finds it within a second.
var udevSettle = 10 * time.Second

// bringUpWait bounds the wait for systemd to mount a pool whose members udev
// evaluated again.
var bringUpWait = 30 * time.Second

// The manager and kernel calls of the bring-up, swapped by tests.
var (
	unitStates      = systemd.GetUnitStates
	devicesReady    = kernelDevicesReady
	startMountUnit  = systemd.StartUnit
	memberSignature = ReadMemberSignature
	peekSignature   = peekMemberSignature
)

// udevDataDir is udev's database of the devices it processed; a var so tests
// can point it at a fixture.
var udevDataDir = "/run/udev/data"

// kernelDevicesReady asks the kernel what udev's "btrfs ready" asks
// (BTRFS_IOC_DEVICES_READY): it registers device and reports whether the
// kernel holds as many registered devices of its filesystem as the
// superblock counts (num_devices == total_devices). A single-device
// filesystem reads ready without being registered.
func kernelDevicesReady(device string) (bool, error) {
	r, err := controlCall(iocDevicesReady, device)
	if err != nil {
		return false, fmt.Errorf("btrfs ready check of %s: %w", device, err)
	}
	return r == 0, nil
}

// unreadyPool is a complete pool systemd waits to mount at boot without
// having found its device, with the mountpoints of its fstab mounts.
type unreadyPool struct {
	pool        BtrfsOfflinePool
	mountpoints []string
}

// unreadyFstabPools returns the unmounted pools with no replace target and
// no disk missing by their superblocks that fstab mounts at boot, whose mount
// unit systemd generated from that fstab file and has not started, and whose
// /dev/disk/by-uuid device systemd has not found. A pool missing a disk or
// holding a replace target is the degraded path's; a device systemd found
// leaves the mount to systemd, done, under way or failed for a reason of its
// own. These are the pools the check waits for; which disks it may touch is
// decided when it acts (udevMembers).
func unreadyFstabPools(ctx context.Context) ([]unreadyPool, error) {
	pools, err := FindOfflinePools(ctx, BlockDevices())
	if err != nil {
		return nil, err
	}
	var unready []unreadyPool
	for _, p := range pools {
		mounts := bootMounts(p.UUID)
		if len(mounts) == 0 || missingDisks(p) > 0 || slices.Contains(p.devids, replaceTargetDevID) {
			continue
		}
		mountpoints, err := awaitedMounts(ctx, p.UUID, mounts)
		if err != nil {
			slog.Warn("failed to read the systemd units of a btrfs pool", "uuid", p.UUID, "error", err)
			continue
		}
		if len(mountpoints) > 0 {
			unready = append(unready, unreadyPool{p, mountpoints})
		}
	}
	return unready, nil
}

// awaitedMounts returns the mountpoints of mounts whose mount unit systemd
// generated from FstabPath and has not started, when systemd has not found
// the device of /dev/disk/by-uuid/<uuid>. A unit the job that gave up left
// unloaded is loaded again to be read.
func awaitedMounts(ctx context.Context, uuid string, mounts []fstabMount) ([]string, error) {
	device, err := systemd.EscapePath("/dev/disk/by-uuid/" + uuid)
	if err != nil {
		return nil, err
	}
	device += ".device"
	names := []string{device}
	unitOf := make([]string, len(mounts)) // "" for a mountpoint that names no unit
	for i, m := range mounts {
		if name, escErr := systemd.EscapePath(m.mountpoint); escErr == nil {
			unitOf[i] = name + ".mount"
			names = append(names, unitOf[i])
		}
	}
	units, err := unitStates(ctx, names)
	if err != nil {
		return nil, err
	}
	if units[device].ActiveState == "active" {
		return nil, nil
	}
	var mountpoints []string
	for i, m := range mounts {
		if u, ok := units[unitOf[i]]; ok && u.LoadState == "loaded" && u.SourcePath == FstabPath && u.ActiveState != "active" {
			mountpoints = append(mountpoints, m.mountpoint)
		}
	}
	return mountpoints, nil
}

// udevMember is a disk of a pool that udev's btrfs rule scanned.
type udevMember struct {
	device string
	devid  uint64
	// ready is the rule's ID_BTRFS_READY: false for a disk it left unready.
	ready bool
}

// udevMembers returns the disks of pool that udev's btrfs rule scanned, and
// whether they are the whole pool: one disk for each of its num_devices
// devids, none a replace target. A disk counts when nothing holds it and
// udev's database names it btrfs of this pool with the ID_BTRFS_READY only
// the rule's builtin sets.
func udevMembers(pool BtrfsOfflinePool) ([]udevMember, bool) {
	var members []udevMember
	devids := map[uint64]bool{}
	whole := true
	for i, device := range pool.Devices {
		name := filepath.Base(device)
		holders, err := os.ReadDir(filepath.Join(sysBlockDir, name, "holders"))
		if err != nil && !errors.Is(err, os.ErrNotExist) || len(holders) > 0 {
			continue
		}
		props, err := udevProperties(name)
		if err != nil {
			slog.Warn("failed to read the udev database entry of a block device", "device", device, "error", err)
			continue
		}
		ready, scanned := props["ID_BTRFS_READY"]
		if !scanned || props["ID_FS_TYPE"] != "btrfs" || props["ID_FS_UUID"] != pool.UUID {
			continue
		}
		devid := pool.devids[i]
		if devid == replaceTargetDevID || devids[devid] {
			whole = false
		}
		devids[devid] = true
		members = append(members, udevMember{device, devid, ready == "1"})
	}
	return members, whole && len(devids) == pool.NumDevices
}

// udevProperties returns the properties (E: lines) of udev's database entry
// for block device name, or none when udev has not processed it.
func udevProperties(name string) (map[string]string, error) {
	dev, err := os.ReadFile(filepath.Join(sysBlockDir, name, "dev"))
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(udevDataDir, "b"+strings.TrimSpace(string(dev))))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	props := map[string]string{}
	for line := range strings.Lines(string(data)) {
		if kv, ok := strings.CutPrefix(strings.TrimSuffix(line, "\n"), "E:"); ok {
			k, v, _ := strings.Cut(kv, "=")
			props[k] = v
		}
	}
	return props, nil
}

// bringUpUnready gives systemd until settle to find the device of each
// complete pool fstab mounts at boot, then brings up those it did not find
// (bringUp). It waits for their mounts, so the services ordered after the
// boot check find the pools, and raises or resolves their alerts.
func bringUpUnready(ctx context.Context, settle time.Duration) error {
	deadline := time.Now().Add(settle)
	for {
		pools, err := unreadyFstabPools(ctx)
		if err != nil || len(pools) == 0 {
			return err
		}
		if !time.Now().Before(deadline) {
			return bringUp(ctx, pools)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(degradedPoll):
		}
	}
}

// bringUp acts on each pool whose disks udev's rule all scanned, none of them
// carrying an md or LVM signature, and whose disks the kernel then calls
// complete: it has udev evaluate again those the rule left unready and asks
// systemd to start the pool's fstab mounts, which joins the boot's own mount
// job while that waits for the device, and starts the mount again when the
// job gave up.
func bringUp(ctx context.Context, pools []unreadyPool) error {
	var errs []error
	var started []unreadyPool
	for _, p := range pools {
		ok, err := bringUpOne(ctx, p)
		if err != nil {
			errs = append(errs, fmt.Errorf("btrfs pool %s: %w", p.pool.UUID, err))
		}
		if ok {
			started = append(started, p)
		}
	}
	up, err := waitMounted(ctx, started)
	if ctx.Err() != nil {
		return errors.Join(append(errs, ctx.Err())...)
	}
	if err != nil {
		// Whether they mounted is unknown: no alert either way.
		slog.Warn("failed to list btrfs mounts", "error", err)
		return errors.Join(errs...)
	}
	for i, p := range started {
		reportBroughtUp(ctx, p, up[i])
	}
	return errors.Join(errs...)
}

// bringUpOne acts on one pool and reports whether it started its mounts. A
// pool it leaves alone is logged; an error is a re-evaluation or a start
// that failed.
func bringUpOne(ctx context.Context, p unreadyPool) (bool, error) {
	// A second device on one devid that udev did not scan leaves the
	// scanned ones whole, so the pool's every device is looked at first.
	reason, readErr := unclearDisks(p.pool, peekSignature)
	if readErr != nil {
		slog.Warn("btrfs pool left alone because the signature of a disk could not be read", "uuid", p.pool.UUID, "error", readErr)
		return false, nil
	}
	if reason != "" {
		slog.Warn("btrfs pool left alone because its disks cannot be told apart", "uuid", p.pool.UUID, "reason", reason)
		return false, nil
	}
	members, whole := udevMembers(p.pool)
	var devices, stale []string
	for _, m := range members {
		devices = append(devices, m.device)
		if !m.ready {
			stale = append(stale, m.device)
		}
	}
	if !whole {
		slog.Warn("btrfs pool left alone because udev did not scan one disk for each of its devids", "uuid", p.pool.UUID, "devices", p.pool.Devices, "scanned", devices)
		return false, nil
	}
	if len(stale) == 0 {
		slog.Warn("btrfs pool left alone because udev marked every disk ready", "uuid", p.pool.UUID, "devices", devices)
		return false, nil
	}
	if err := checkNoMemberSignature(devices); err != nil {
		slog.Warn("btrfs pool left alone because a disk may belong to an array or volume group", "uuid", p.pool.UUID, "error", err)
		return false, nil
	}
	ready, err := kernelReady(devices)
	if err != nil {
		slog.Warn("btrfs ready check failed", "uuid", p.pool.UUID, "error", err)
		return false, nil
	}
	if !ready {
		slog.Warn("btrfs pool not complete in the kernel", "uuid", p.pool.UUID, "devices", devices)
		return false, nil
	}
	if err := reevaluate(stale); err != nil {
		return false, err
	}
	if err := startMounts(ctx, p.mountpoints); err != nil {
		return false, err
	}
	slog.Debug("btrfs pool disks evaluated again", "uuid", p.pool.UUID, "devices", stale)
	return true, nil
}

// checkNoMemberSignature reads each device's md and LVM signature and fails
// for one that has one, or that cannot be read: a disk udev calls btrfs may
// still be a member that udev saw before its array or label was written.
func checkNoMemberSignature(devices []string) error {
	for _, d := range devices {
		sig, err := memberSignature(d)
		if err != nil {
			return err
		}
		if sig.Kind != "" {
			return fmt.Errorf("%s carries an %s signature", d, sig.Kind)
		}
	}
	return nil
}

// partOfAnother says what makes device, which shows a btrfs superblock, part
// of another device rather than a disk of that filesystem, or returns "".
// md binds a leg of an array (md.c bind_rdev_to_array) and device-mapper
// each device under a map, an LVM volume, a multipath disk or dm-crypt
// (dm.c open_table_device): both claim it and link it as their holder, so
// btrfs could not open it and it is no disk; nor is a partition of a disk
// something holds, which the kernel lets nothing claim (bdev.c
// bd_may_claim), such as a path's partition under a multipath map. Nothing
// holds a leg of an array that has not been assembled or a path of a
// multipath disk whose map does not exist yet; libblkid reports their md,
// LVM or multipath membership instead of the btrfs superblock
// (superblocks.c superblocks_safeprobe stops at a RAID or LVM signature;
// multipath's rule names a path mpath_member), which udev's database
// records, so udev's 64-btrfs.rules never registers them, and neither does
// btrfs-progs' device scan, which registers only what libblkid calls btrfs.
// A device udev has no entry for, not processed yet or under a disk lock
// LinuxIO holds, is judged by its own md and LVM signature, read as libblkid
// reads it, through a plain open that claims nothing. A device udev probed
// as anything else stays a disk: every write refuses a disk of a pool, and
// what is left out here is refused as held or as the member it is
// (docs/btrfs.md, "Which disks a pool may take").
func partOfAnother(device string) string {
	name := filepath.Base(device)
	holders, err := os.ReadDir(filepath.Join(sysBlockDir, name, "holders"))
	if len(holders) > 0 {
		return "is held by /dev/" + holders[0].Name()
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("failed to read the holders of a block device", "device", device, "error", err)
	}
	// A partition's sysfs directory sits in its disk's.
	if _, statErr := os.Stat(filepath.Join(sysBlockDir, name, "partition")); statErr == nil {
		if dir, linkErr := filepath.EvalSymlinks(filepath.Join(sysBlockDir, name)); linkErr == nil {
			disk := filepath.Dir(dir)
			if holders, _ := os.ReadDir(filepath.Join(disk, "holders")); len(holders) > 0 {
				return "is a partition of /dev/" + filepath.Base(disk) + ", held by /dev/" + holders[0].Name()
			}
		}
	}
	props, err := udevProperties(name)
	if err != nil {
		slog.Warn("failed to read the udev database entry of a block device", "device", device, "error", err)
		return ""
	}
	if props != nil {
		if fsType := props["ID_FS_TYPE"]; strings.HasSuffix(fsType, "_member") {
			return "udev probed as " + fsType
		}
		return ""
	}
	sig, err := peekSignature(device)
	if err != nil {
		slog.Warn("failed to read the md and LVM signature of a block device", "device", device, "error", err)
		return ""
	}
	if sig.Kind != "" {
		return "carries an " + sig.Kind + " signature"
	}
	return ""
}

// unclearDisks says why LinuxIO cannot tell that each present device of
// pool is a disk of it, or returns "", with read for each device's md and
// LVM signature, whose error it returns. Two devices that carry one devid
// are two paths of one disk before multipath maps them, or a disk and a copy
// of it, and the kernel keeps whichever it scanned last or whose generation
// is newer (volumes.c device_list_add). A path of a multipath disk, which
// udev marks with DM_MULTIPATH_DEVICE_PATH, is skipped by btrfs-progs'
// device scan (device-scan.c is_multipath_path_device). And a device whose
// md or LVM signature shows is a member that udev probed before its array
// or label was written.
func unclearDisks(pool BtrfsOfflinePool, read func(string) (MemberSignature, error)) (string, error) {
	for i, d := range pool.Devices {
		if j := slices.Index(pool.devids, pool.devids[i]); j < i {
			return fmt.Sprintf("%s and %s both carry disk %d of the pool, so LinuxIO cannot tell which one is the pool's", pool.Devices[j], d, pool.devids[i]), nil
		}
	}
	for _, d := range pool.Devices {
		props, err := udevProperties(filepath.Base(d))
		if err != nil {
			slog.Warn("failed to read the udev database entry of a block device", "device", d, "error", err)
			continue
		}
		if v := props["DM_MULTIPATH_DEVICE_PATH"]; v != "" && v != "0" {
			return "udev marks " + d + " a path of a multipath disk, which only its multipath device may use", nil
		}
	}
	for _, d := range pool.Devices {
		sig, err := read(d)
		if err != nil {
			return "", err
		}
		if sig.Kind != "" {
			return fmt.Sprintf("%s carries an %s signature", d, sig.Kind), nil
		}
	}
	return "", nil
}

// mountState is what the check found of a pool it started the mounts of.
type mountState int

const (
	mountedNone mountState = iota
	mountedSome
	mountedAll
)

// reportBroughtUp logs what became of pool, whose mounts the check started,
// and raises or resolves its alerts: the offline alert only for a pool known
// not mounted and whose mount systemd is not still making.
func reportBroughtUp(ctx context.Context, p unreadyPool, state mountState) {
	args := []any{"uuid", p.pool.UUID, "devices", p.pool.Devices, "mountpoints", p.mountpoints}
	switch state {
	case mountedAll:
		slog.Info("btrfs pool mounted after udev evaluated its disks again", args...)
	case mountedSome:
		slog.Warn("btrfs pool not mounted everywhere after udev evaluated its disks again", args...)
	default:
		mounting, err := stillMounting(ctx, p)
		switch {
		case err != nil:
			slog.Warn("failed to read the systemd units of a btrfs pool", append(args, "error", err)...)
		case mounting:
			slog.Info("btrfs pool device found after udev evaluated its disks again", args...)
		default:
			slog.Warn("btrfs pool not mounted after udev evaluated its disks again", args...)
			RaiseOfflineAlert(ctx, p.pool, "")
		}
		return
	}
	fs, err := FindFilesystem(ctx, p.pool.UUID)
	if err != nil {
		slog.Warn("failed to read a mounted btrfs pool", "uuid", p.pool.UUID, "error", err)
		return
	}
	RaiseAlerts(ctx, fs)
}

// stillMounting reports whether systemd found pool's device since or is
// still mounting one of its mountpoints.
func stillMounting(ctx context.Context, p unreadyPool) (bool, error) {
	device, err := systemd.EscapePath("/dev/disk/by-uuid/" + p.pool.UUID)
	if err != nil {
		return false, err
	}
	names := []string{device + ".device"}
	for _, mnt := range p.mountpoints {
		if name, escErr := systemd.EscapePath(mnt); escErr == nil {
			names = append(names, name+".mount")
		}
	}
	units, err := unitStates(ctx, names)
	if err != nil {
		return false, err
	}
	for _, name := range names {
		if u := units[name]; u.ActiveState == "activating" || strings.HasSuffix(name, ".device") && u.ActiveState == "active" {
			return true, nil
		}
	}
	return false, nil
}

// startMounts asks systemd to start the mount unit of each mountpoint.
func startMounts(ctx context.Context, mountpoints []string) error {
	for _, mnt := range mountpoints {
		name, err := systemd.EscapePath(mnt)
		if err == nil {
			err = startMountUnit(ctx, name+".mount")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// kernelReady asks the kernel about each device, as udev's rule does. The
// last answer counts: by then every one is registered, so it is the kernel's
// count for the whole pool.
func kernelReady(devices []string) (bool, error) {
	ready := false
	for _, d := range devices {
		r, err := devicesReady(d)
		if err != nil {
			return false, err
		}
		ready = r
	}
	return ready, nil
}

// reevaluate has the kernel send a change uevent for each device, so udev
// runs its rules for it again, as `udevadm trigger` does. A device's uevent
// attribute is opened as it is, never created.
func reevaluate(devices []string) error {
	for _, d := range devices {
		f, err := os.OpenFile(filepath.Join(sysBlockDir, filepath.Base(d), "uevent"), os.O_WRONLY, 0)
		if err != nil {
			return fmt.Errorf("re-evaluate %s: %w", d, err)
		}
		_, err = f.WriteString("change")
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("re-evaluate %s: %w", d, err)
		}
	}
	return nil
}

// waitMounted waits up to bringUpWait for each pool to be mounted at every
// mountpoint it was started at, and reports how far each got. An error means
// the mounts could not be listed, so it is not known.
func waitMounted(ctx context.Context, pools []unreadyPool) ([]mountState, error) {
	states := make([]mountState, len(pools))
	if len(pools) == 0 {
		return states, nil
	}
	deadline := time.Now().Add(bringUpWait)
	for {
		mounts, err := listMounts(ctx)
		if err != nil {
			return nil, err
		}
		all := true
		for i, p := range pools {
			states[i] = mountStateOf(p, mounts)
			all = all && states[i] == mountedAll
		}
		if all || !time.Now().Before(deadline) {
			return states, nil
		}
		select {
		case <-ctx.Done():
			return states, nil
		case <-time.After(degradedPoll):
		}
	}
}

// mountStateOf says how much of pool mounts shows mounted.
func mountStateOf(p unreadyPool, mounts []Mount) mountState {
	n := 0
	for _, mnt := range p.mountpoints {
		if slices.ContainsFunc(mounts, func(m Mount) bool { return m.UUID == p.pool.UUID && m.Mountpoint == filepath.Clean(mnt) }) {
			n++
		}
	}
	switch {
	case n == len(p.mountpoints):
		return mountedAll
	case n > 0 || slices.ContainsFunc(mounts, func(m Mount) bool { return m.UUID == p.pool.UUID }):
		return mountedSome
	}
	return mountedNone
}
