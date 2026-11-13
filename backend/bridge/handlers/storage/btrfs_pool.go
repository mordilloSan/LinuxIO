package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

// btrfsPoolFstabOptions are the mount options of a pool LinuxIO records in
// /etc/fstab; nofail keeps a pool with a missing device from blocking boot.
var btrfsPoolFstabOptions = []string{"defaults", "noatime", "nofail"}

// CreateBtrfsPool validates the request, formats the member devices, mounts the
// new pool and records it in /etc/fstab, reporting each step and, while it
// trims, the percent done. A failure before the pool is in fstab undoes what
// the call made (see poolCreateUndo).
func CreateBtrfsPool(ctx context.Context, req apischema.BtrfsPoolCreateRequest, report func(step string, pct float64)) (_ btrfs.BtrfsFilesystem, err error) {
	inv, err := loadPartitionInventory(ctx)
	if err != nil {
		return btrfs.BtrfsFilesystem{}, err
	}
	if err = checkPoolCreate(req, inv, getFstabEntries()); err != nil {
		return btrfs.BtrfsFilesystem{}, err
	}
	// Mounting over files would hide them.
	if entries, readErr := os.ReadDir(req.Mountpoint); readErr == nil && len(entries) > 0 {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("mountpoint %s is not empty", req.Mountpoint)
	}
	slog.Debug("creating btrfs pool", "label", req.Label, "devices", req.Devices, "freeSpace", req.FreeSpace, "data", req.DataProfile, "metadata", req.MetadataProfile, "mountpoint", req.Mountpoint, "force", req.Force, "partition", req.Partition)
	made := &poolCreateUndo{members: slices.Clone(req.Devices)}
	defer func() { err = made.finish(ctx, req.Label, err) }()
	if req.Partition || len(req.FreeSpace) > 0 {
		report("Partitioning disks", 0)
	}
	if req.Partition {
		made.members = make([]string, 0, len(req.Devices))
		for _, d := range req.Devices {
			part, unlock, partErr := partitionLocked(ctx, d, partitionDisk)
			if partErr != nil {
				return btrfs.BtrfsFilesystem{}, fmt.Errorf("partition %s for btrfs pool: %w", d, partErr)
			}
			made.record(d, part, unlock)
		}
	}
	for _, d := range req.FreeSpace {
		part, unlock, partErr := partitionLocked(ctx, d, addPartition)
		if partErr != nil {
			return btrfs.BtrfsFilesystem{}, fmt.Errorf("add partition on %s for btrfs pool: %w", d, partErr)
		}
		made.record(d, part, unlock)
	}
	made.uuid, err = createBtrfsPool(ctx, req.Label, req.DataProfile, req.MetadataProfile, made.members, req.Force, formatProgress(report))
	if err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("create btrfs pool %q: %w", req.Label, err)
	}
	report("Mounting at "+req.Mountpoint, 0)
	if err = mountBtrfsPool(ctx, made.members[0], req.Mountpoint, btrfsPoolFstabOptions, false); err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("mount btrfs pool %s: %w", made.uuid, err)
	}
	made.mounted = req.Mountpoint
	if err = addPoolFstab("UUID="+made.uuid, req.Mountpoint, "btrfs", btrfsPoolFstabOptions); err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("add btrfs pool %s to /etc/fstab: %w", made.uuid, err)
	}
	made.committed = true
	slog.Info("created btrfs pool", "uuid", made.uuid, "mountpoint", req.Mountpoint)
	return findBtrfsFilesystem(ctx, made.uuid)
}

// poolCreateUndo is what a CreateBtrfsPool call made, so that a failure
// before the pool is in fstab can undo it while the disk locks are held.
type poolCreateUndo struct {
	members   []string
	unlocks   []func()
	created   []createdPartition // in the order made
	uuid      string             // set once mkfs.btrfs ran
	mounted   string             // the mountpoint, once the call mounted the pool
	committed bool               // the pool is in fstab: it is kept whatever fails after
}

// createdPartition is a partition CreateBtrfsPool made on disk.
type createdPartition struct{ disk, part string }

// record notes part, made on disk, as a member the undo deletes and
// unlock as the release of disk's lock.
func (u *poolCreateUndo) record(disk, part string, unlock func()) {
	u.members = append(u.members, part)
	u.created = append(u.created, createdPartition{disk, part})
	u.unlocks = append(u.unlocks, unlock)
}

// finish undoes the create when it failed with err before the pool reached
// fstab, joining any undo error to err, then releases the disk locks.
func (u *poolCreateUndo) finish(ctx context.Context, label string, err error) error {
	if err != nil && !u.committed && (u.uuid != "" || len(u.created) > 0) {
		// The undo runs to the end even when the job was canceled.
		undoCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		undoErr := u.undo(undoCtx)
		cancel()
		if undoErr != nil {
			err = errors.Join(err, fmt.Errorf("undo btrfs pool create: %w", undoErr))
		} else {
			slog.Info("rolled back failed btrfs pool create", "label", label, "uuid", u.uuid, "partitions", len(u.created))
		}
	}
	for _, unlock := range slices.Backward(u.unlocks) {
		unlock()
	}
	return err
}

// undo unmounts the pool if the call mounted it, zeroes the superblocks
// mkfs.btrfs wrote with the pool's UUID on the members, then deletes the
// partitions the call made, newest first. Nothing is touched while the pool
// stays mounted, and a partition whose wipe failed is kept: its superblock
// offsets are relative to the partition.
func (u *poolCreateUndo) undo(ctx context.Context) error {
	if u.mounted != "" {
		if err := unmountPool(u.mounted, 0); err != nil {
			return fmt.Errorf("unmount %s: %w", u.mounted, err)
		}
	}
	var errs []error
	kept := map[string]bool{}
	if u.uuid != "" {
		for _, m := range u.members {
			if _, err := wipePoolSuperblocks(m, u.uuid); err != nil {
				errs = append(errs, fmt.Errorf("wipe %s: %w", m, err))
				kept[m] = true
			}
		}
	}
	for _, c := range slices.Backward(u.created) {
		if kept[c.part] {
			continue
		}
		number, err := partitionNumber(c.part)
		if err == nil {
			err = deletePartition(ctx, c.disk, number)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("delete %s: %w", c.part, err))
		}
	}
	return errors.Join(errs...)
}

// partitionLocked locks disk against udev, then partitions it. The caller
// holds the lock until the pool is made, so the disk's partition nodes do
// not flap under mkfs.btrfs.
func partitionLocked(ctx context.Context, disk string, partition func(context.Context, string) (string, error)) (string, func(), error) {
	unlock, err := lockDisk(ctx, disk)
	if err != nil {
		return "", nil, err
	}
	part, err := partition(ctx, disk)
	if err != nil {
		unlock()
		// A disk something claimed since the check is a conflict, not a fault.
		return "", nil, conflictIfBusy(err)
	}
	return part, unlock, nil
}

// checkPoolCreate validates a create request against the host: label,
// profiles for the device count, every device free (or, for FreeSpace, a
// disk not in use with room for a partition), and an unused mountpoint
// outside the system directories.
func checkPoolCreate(req apischema.BtrfsPoolCreateRequest, inv partitionInventory, fstab map[string]fstabEntry) error {
	if err := btrfs.ValidateLabel(req.Label); err != nil {
		return err
	}
	if len(req.Devices)+len(req.FreeSpace) == 0 {
		return errors.New("select at least one device")
	}
	for i, d := range req.Devices {
		if slices.Contains(req.Devices[:i], d) {
			return fmt.Errorf("device %s listed twice", d)
		}
		if err := checkBtrfsDeviceFree(inv, d, req.Force); err != nil {
			return err
		}
	}
	for i, d := range req.FreeSpace {
		if slices.Contains(req.FreeSpace[:i], d) || slices.Contains(req.Devices, d) {
			return fmt.Errorf("device %s listed twice", d)
		}
		disk, err := inv.device(d)
		if err != nil {
			return err
		}
		if disk.Type != "disk" || disk.FreeBytes == 0 {
			return fmt.Errorf("%w: device %s has no free space for a partition", bridgeipc.ErrInvalidArgs, d)
		}
		// As for Create partition: the disk's other partitions may be in
		// use, the disk itself not.
		if err = deviceInUse(disk, inv.fss, inv.offline, false, false); err != nil {
			return err
		}
	}
	data, meta := req.DataProfile, req.MetadataProfile
	if err := btrfs.ValidateProfiles(&data, &meta, len(req.Devices)+len(req.FreeSpace)); err != nil {
		return err
	}
	return checkPoolMountpoint(req.Mountpoint, inv.devices, fstab)
}

func checkPoolMountpoint(mountpoint string, devices []apischema.StorageBlockDevice, fstab map[string]fstabEntry) error {
	if !validPath.MatchString(mountpoint) || filepath.Clean(mountpoint) != mountpoint {
		return fmt.Errorf("%w: invalid mountpoint %q", bridgeipc.ErrInvalidArgs, mountpoint)
	}
	if isSystemPath(mountpoint) || isProtectedMount(mountpoint) {
		return fmt.Errorf("%w: mountpoint %s is a system directory", bridgeipc.ErrInvalidArgs, mountpoint)
	}
	if _, ok := fstab[mountpoint]; ok {
		return fmt.Errorf("%w: mountpoint %s is already in /etc/fstab", bridgeipc.ErrConflict, mountpoint)
	}
	for _, d := range devices {
		if slices.Contains(d.Mountpoints, mountpoint) {
			return fmt.Errorf("%w: mountpoint %s is in use by %s", bridgeipc.ErrConflict, mountpoint, d.Path)
		}
	}
	return nil
}

// DestroyBtrfsPool unmounts the pool, drops its fstab entries and wipes its
// devices.
func DestroyBtrfsPool(ctx context.Context, uuid string) error {
	// Listed before the unmount: a mount is how fstab lines that do not say
	// UUID= are tied to the pool.
	mounts, err := btrfsListMounts(ctx)
	if err != nil {
		return fmt.Errorf("destroy btrfs pool %s: list btrfs mounts: %w", uuid, err)
	}
	devices, err := poolDevicesForDestroy(ctx, uuid)
	if err != nil {
		return fmt.Errorf("destroy btrfs pool %s: %w", uuid, err)
	}
	slog.Debug("destroying btrfs pool", "uuid", uuid, "devices", devices)
	if fstabErr := removePoolFstabEntries(uuid, mounts); fstabErr != nil {
		return fstabErr
	}
	for _, d := range devices {
		if err := btrfs.WipeDevice(d); err != nil {
			return fmt.Errorf("wipe %s of btrfs pool %s: %w", d, uuid, err)
		}
	}
	slog.Info("destroyed btrfs pool", "uuid", uuid)
	return nil
}

// removePoolFstabEntries drops every fstab line that mounted pool uuid: by
// mountpoint, by UUID=, or by /dev/disk/by-uuid/ (which also covers an offline
// pool).
func removePoolFstabEntries(uuid string, mounts []btrfs.Mount) error {
	entries := btrfsFstabEntries(uuid, mounts)
	for mountpoint, entry := range getFstabEntries() {
		if entry.fstype == "btrfs" && strings.EqualFold(entry.source, "/dev/disk/by-uuid/"+uuid) {
			entries[mountpoint] = entry
		}
	}
	for mountpoint := range entries {
		if err := removeFromFstab(mountpoint); err != nil {
			return fmt.Errorf("remove %s from /etc/fstab: %w", mountpoint, err)
		}
	}
	return nil
}

// poolDevicesForDestroy unmounts a mounted pool and returns its present
// devices, or the devices of an offline pool.
func poolDevicesForDestroy(ctx context.Context, uuid string) ([]string, error) {
	filesystems, err := btrfs.ListFilesystems(ctx)
	if err != nil {
		return nil, err
	}
	if i := slices.IndexFunc(filesystems, func(fs btrfs.BtrfsFilesystem) bool { return fs.UUID == uuid }); i >= 0 {
		fs := filesystems[i]
		mountpoints, mountErr := poolMountpoints(fs)
		if mountErr != nil {
			return nil, mountErr
		}
		for _, m := range mountpoints {
			if err = unix.Unmount(m, 0); err != nil {
				return nil, fmt.Errorf("unmount %s: %w", m, err)
			}
		}
		var devices []string
		for _, d := range fs.Devices {
			if d.Path != "" {
				devices = append(devices, d.Path)
			}
		}
		return devices, nil
	}
	pool, err := findOfflinePool(ctx, uuid)
	if err != nil {
		return nil, err
	}
	return pool.Devices, nil
}

// poolMountpoints returns the filesystem's distinct mountpoints deepest
// first, so nested mounts unmount before their parents, refusing a pool
// that holds a protected system mount. LinuxIO's own top-level mount of the
// pool (btrfs.TopLevel, under /run/linuxio/btrfs), which a subvolume,
// snapshot or settings read leaves behind, is unmounted with the rest:
// otherwise reading a pool would keep it from being destroyed.
func poolMountpoints(fs btrfs.BtrfsFilesystem) ([]string, error) {
	helper := filepath.Join(btrfs.TopLevelDir, fs.UUID)
	var mountpoints []string
	for _, m := range fs.Mountpoints {
		if isProtectedMount(m.Mountpoint) && (fs.UUID == "" || m.Mountpoint != helper) {
			return nil, fmt.Errorf("refusing to destroy a pool mounted at %s", m.Mountpoint)
		}
		if !slices.Contains(mountpoints, m.Mountpoint) {
			mountpoints = append(mountpoints, m.Mountpoint)
		}
	}
	sort.SliceStable(mountpoints, func(i, j int) bool { return len(mountpoints[i]) > len(mountpoints[j]) })
	return mountpoints, nil
}

// MountBtrfsPool mounts an unmounted pool with its fstab options, adding an
// fstab entry when it has none.
func MountBtrfsPool(ctx context.Context, req apischema.BtrfsPoolMountRequest) (btrfs.BtrfsFilesystem, error) {
	pool, err := findOfflinePool(ctx, req.UUID)
	if err != nil {
		return btrfs.BtrfsFilesystem{}, err
	}
	if pool.Missing > 0 && !req.Degraded {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("%w: pool %s is missing %d of %d devices; mount it degraded to use it anyway", bridgeipc.ErrConflict, pool.UUID, pool.Missing, pool.NumDevices)
	}
	mountpoint := pool.Mountpoint
	persist := mountpoint == ""
	options := btrfsPoolFstabOptions
	if persist {
		devices, err := ListBlockDevices(ctx)
		if err != nil {
			return btrfs.BtrfsFilesystem{}, err
		}
		mountpoint = defaultPoolMountpoint(pool)
		if err := checkPoolMountpoint(mountpoint, devices, getFstabEntries()); err != nil {
			return btrfs.BtrfsFilesystem{}, err
		}
	} else {
		// subvol=, compress= and the like decide what the mount shows.
		options = strings.Split(getFstabEntries()[mountpoint].options, ",")
	}
	if err := mountOfflinePool(ctx, pool, mountpoint, options, req.Degraded); err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("mount btrfs pool %s at %s: %w", pool.UUID, mountpoint, err)
	}
	if persist {
		if err := addToFstab("UUID="+pool.UUID, mountpoint, "btrfs", btrfsPoolFstabOptions); err != nil {
			return btrfs.BtrfsFilesystem{}, fmt.Errorf("pool %s is mounted but could not be added to /etc/fstab: %w", pool.UUID, err)
		}
	}
	slog.Info("btrfs pool mounted", "uuid", pool.UUID, "mountpoint", mountpoint, "degraded", req.Degraded)
	return btrfs.FindFilesystem(ctx, pool.UUID)
}

// defaultPoolMountpoint is /mnt/<label>, or /mnt/<uuid> when the label does
// not make a plain path.
func defaultPoolMountpoint(pool btrfs.BtrfsOfflinePool) string {
	if mountpoint := "/mnt/" + pool.Label; validPath.MatchString(mountpoint) && filepath.Clean(mountpoint) == mountpoint {
		return mountpoint
	}
	return "/mnt/" + pool.UUID
}

// ListOfflinePools finds the unmounted btrfs filesystems by their
// superblocks and attaches their fstab mountpoints.
func ListOfflinePools(ctx context.Context) ([]btrfs.BtrfsOfflinePool, error) {
	pools, err := btrfs.FindOfflinePools(ctx, btrfs.BlockDevices())
	if err != nil {
		return nil, err
	}
	fstab := getFstabEntries()
	for i := range pools {
		pools[i].Mountpoint = fstabMountpointFor(fstab, pools[i].UUID)
	}
	return pools, nil
}

// GrowBtrfsDevice resizes a device to fill its backing block device.
func GrowBtrfsDevice(ctx context.Context, req apischema.BtrfsDeviceRequest) (btrfs.BtrfsFilesystem, error) {
	if err := btrfs.GrowDevice(ctx, req.UUID, req.DeviceID); err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("grow btrfs device %d of %s: %w", req.DeviceID, req.UUID, err)
	}
	slog.Info("btrfs device grown", "uuid", req.UUID, "device", req.DeviceID)
	return btrfs.FindFilesystem(ctx, req.UUID)
}

// SetBtrfsLabel relabels a btrfs filesystem.
func SetBtrfsLabel(ctx context.Context, req apischema.BtrfsLabelRequest) (btrfs.BtrfsFilesystem, error) {
	if err := btrfs.SetLabel(ctx, req.UUID, req.Label); err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("relabel btrfs filesystem %s to %q: %w", req.UUID, req.Label, err)
	}
	slog.Info("btrfs filesystem relabeled", "uuid", req.UUID, "label", req.Label)
	return btrfs.FindFilesystem(ctx, req.UUID)
}

func findOfflinePool(ctx context.Context, uuid string) (btrfs.BtrfsOfflinePool, error) {
	pools, err := offlinePools(ctx)
	if err != nil {
		return btrfs.BtrfsOfflinePool{}, err
	}
	i := slices.IndexFunc(pools, func(p btrfs.BtrfsOfflinePool) bool { return p.UUID == uuid })
	if i < 0 {
		return btrfs.BtrfsOfflinePool{}, fmt.Errorf("%w: unmounted btrfs filesystem %s", bridgeipc.ErrNotFound, uuid)
	}
	return pools[i], nil
}

// fstabMountpointFor returns the first fstab mountpoint, in path order, whose
// source is the filesystem's UUID.
func fstabMountpointFor(entries map[string]fstabEntry, uuid string) string {
	mountpoints := make([]string, 0, len(entries))
	for m, e := range entries {
		if strings.EqualFold(e.source, "UUID="+uuid) {
			mountpoints = append(mountpoints, m)
		}
	}
	if len(mountpoints) == 0 {
		return ""
	}
	return slices.Min(mountpoints)
}
