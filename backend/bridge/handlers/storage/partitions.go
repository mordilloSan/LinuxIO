package storage

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

// Seams for tests: the inventory the rules check, and every call that locks,
// probes or writes a device.
var (
	storageBlockDevices = ListBlockDevices
	btrfsFilesystems    = btrfs.ListFilesystems
	offlinePools        = ListOfflinePools
	createPartition     = btrfs.CreatePartition
	deletePartition     = btrfs.DeletePartition
	initializeDisk      = btrfs.InitializeDisk
	formatDevice        = btrfs.Format
	checkNotBusy        = btrfs.CheckNotBusy
	mountDevice         = unix.Mount
	lockDisk            = btrfs.LockDisk
)

// formatStep is the progress step of every format: mkfs discards the whole
// partition first, which is why create and format run as tasks.
const formatStep = "Formatting: SSDs are trimmed first, which can take a few minutes"

// callLockWait bounds how long a call waits for the disk lock: 5 s covers
// udevd's brief shared lock while it probes, without hanging the request
// behind a long mkfs. A var so tests can shorten it.
var callLockWait = 5 * time.Second

var partitionRoutes = partitionBindings().Routes()

func partitionBindings() apischema.BindingSet {
	return apischema.Bindings(
		apischema.TaskRunner[apischema.PartitionCreateRequest, apischema.SuccessPathResponse]("storage.partition_create", apischema.Privileged(), apischema.SessionTask(), apischema.WithTaskProgress[BtrfsOpProgress](), apischema.WithTaskMetadata(func(req apischema.PartitionCreateRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: []string{req.Device, "partition"}, Label: "Creating partition", Device: req.Device}
		})).Run(runPartitionCreateTask, bridgeipc.TaskDefault),
		apischema.TaskRunner[apischema.PartitionFormatRequest, apischema.SuccessResponse]("storage.partition_format", apischema.Privileged(), apischema.SessionTask(), apischema.WithTaskProgress[BtrfsOpProgress](), apischema.WithTaskMetadata(func(req apischema.PartitionFormatRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: []string{req.Device, "format"}, Label: "Formatting partition", Device: req.Device}
		})).Run(runPartitionFormatTask, bridgeipc.TaskDefault),
	)
}

func runPartitionCreateTask(ctx context.Context, task *bridgeipc.Task, req apischema.PartitionCreateRequest) (apischema.SuccessPathResponse, error) {
	part, err := CreatePartition(ctx, req, func(step string) {
		task.ReportProgress(BtrfsOpProgress{Op: "partition", Message: step})
	})
	if err != nil {
		return apischema.SuccessPathResponse{}, err
	}
	return apischema.SuccessPathResponse{Success: true, Path: part}, nil
}

func runPartitionFormatTask(ctx context.Context, task *bridgeipc.Task, req apischema.PartitionFormatRequest) (apischema.SuccessResponse, error) {
	err := FormatPartition(ctx, req, func(step string) {
		task.ReportProgress(BtrfsOpProgress{Op: "format", Message: step})
	})
	if err != nil {
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}

// partitionInventory is what every request is checked against, read fresh:
// the UI's disabled states are only hints.
type partitionInventory struct {
	devices []apischema.StorageBlockDevice
	fss     []btrfs.BtrfsFilesystem
	offline []btrfs.BtrfsOfflinePool
}

func loadPartitionInventory(ctx context.Context) (partitionInventory, error) {
	var inv partitionInventory
	var err error
	if inv.devices, err = storageBlockDevices(ctx); err != nil {
		return inv, err
	}
	if inv.fss, err = btrfsFilesystems(ctx); err != nil {
		return inv, err
	}
	inv.offline, err = offlinePools(ctx)
	return inv, err
}

func (inv partitionInventory) device(path string) (apischema.StorageBlockDevice, error) {
	i := slices.IndexFunc(inv.devices, func(d apischema.StorageBlockDevice) bool { return d.Path == path })
	if i < 0 {
		return apischema.StorageBlockDevice{}, fmt.Errorf("%w: block device %s", bridgeipc.ErrNotFound, path)
	}
	return inv.devices[i], nil
}

// writableDisk finds path, a whole disk with a medium that is not read-only.
func (inv partitionInventory) writableDisk(path string) (apischema.StorageBlockDevice, error) {
	disk, err := inv.device(path)
	switch {
	case err != nil:
		return disk, err
	case disk.Type != "disk":
		return disk, fmt.Errorf("%w: %s is not a whole disk", bridgeipc.ErrInvalidArgs, path)
	case disk.SizeBytes == 0:
		return disk, fmt.Errorf("%w: %s has no medium", bridgeipc.ErrInvalidArgs, path)
	case disk.ReadOnly:
		return disk, fmt.Errorf("%w: %s is read-only", bridgeipc.ErrConflict, path)
	}
	return disk, nil
}

// writablePartition finds path, a partition, and its disk, which must not be
// read-only.
func (inv partitionInventory) writablePartition(path string) (part, disk apischema.StorageBlockDevice, err error) {
	if part, err = inv.device(path); err != nil {
		return part, disk, err
	}
	if part.Type != "part" || len(part.Parents) == 0 {
		return part, disk, fmt.Errorf("%w: %s is not a partition", bridgeipc.ErrInvalidArgs, path)
	}
	if disk, err = inv.device(part.Parents[0]); err != nil {
		return part, disk, err
	}
	if disk.ReadOnly {
		return part, disk, fmt.Errorf("%w: %s is read-only", bridgeipc.ErrConflict, disk.Path)
	}
	return part, disk, nil
}

// deviceInUse refuses dev when it is mounted, an active swap or held (Busy),
// a member of a btrfs filesystem mounted or not, or named in /etc/fstab. A
// mount just for now (forMount without persist) ignores /etc/fstab, so a
// partition listed there can still be mounted.
func deviceInUse(dev apischema.StorageBlockDevice, fss []btrfs.BtrfsFilesystem, offline []btrfs.BtrfsOfflinePool, forMount, persist bool) error {
	inUse := func(reason string) error {
		return fmt.Errorf("%w: %s: %s", bridgeipc.ErrConflict, dev.Path, reason)
	}
	if dev.Busy != "" {
		return inUse(dev.Busy)
	}
	for _, fs := range fss {
		if slices.ContainsFunc(fs.Devices, func(b btrfs.BtrfsDevice) bool { return b.Path == dev.Path }) {
			return inUse("Member of btrfs pool " + cmp.Or(fs.Label, fs.UUID))
		}
	}
	for _, pool := range offline {
		if slices.Contains(pool.Devices, dev.Path) {
			return inUse("Member of btrfs pool " + cmp.Or(pool.Label, pool.UUID))
		}
	}
	if dev.InFstab && (!forMount || persist) {
		return inUse("In /etc/fstab")
	}
	return nil
}

// lockDiskForCall is lockDisk for a call, which gives up after callLockWait
// with a conflict instead of waiting out the request. An error of a request
// that ended on its own comes back unchanged.
func lockDiskForCall(ctx context.Context, disk string) (func(), error) {
	waitCtx, cancel := context.WithTimeout(ctx, callLockWait)
	defer cancel()
	unlock, err := lockDisk(waitCtx, disk)
	if err != nil && waitCtx.Err() != nil && ctx.Err() == nil {
		return nil, fmt.Errorf("%w: another operation is running on %s", bridgeipc.ErrConflict, disk)
	}
	return unlock, err
}

// conflictIfBusy marks an error of the btrfs layer as a conflict when the
// kernel found the device in use, Format found data udev had not reported,
// or the kernel's partition table is older than the disk's.
func conflictIfBusy(err error) error {
	if errors.Is(err, unix.EBUSY) || errors.Is(err, btrfs.ErrDeviceHasData) || errors.Is(err, btrfs.ErrStaleTable) {
		return fmt.Errorf("%w: %w", bridgeipc.ErrConflict, err)
	}
	return err
}

// checkMountpoint applies the pool mountpoint rules, consulting /etc/fstab
// only for a mount at boot, and refuses a directory whose files the mount
// would hide.
func checkMountpoint(mountpoint string, persist bool, devices []apischema.StorageBlockDevice) error {
	var fstab map[string]fstabEntry
	if persist {
		fstab = getFstabEntries()
	}
	if err := checkPoolMountpoint(mountpoint, devices, fstab); err != nil {
		return err
	}
	if entries, err := os.ReadDir(mountpoint); err == nil && len(entries) > 0 {
		return fmt.Errorf("%w: mountpoint %s is not empty", bridgeipc.ErrConflict, mountpoint)
	}
	return nil
}

// mountFilesystem mounts device at mountpoint, creating the directory.
func mountFilesystem(device, fsType, mountpoint string) error {
	if err := os.MkdirAll(mountpoint, 0o755); err != nil {
		return fmt.Errorf("create mountpoint: %w", err)
	}
	if err := mountDevice(device, mountpoint, fsType, 0, ""); err != nil {
		return fmt.Errorf("mount %s at %s: %w", device, mountpoint, err)
	}
	return nil
}

// persistMount, with persist, records device, just mounted at mountpoint, in
// /etc/fstab by its UUID; nofail keeps a missing disk from blocking boot.
func persistMount(persist bool, device, uuid, fsType, mountpoint string) error {
	if !persist {
		return nil
	}
	if err := addToFstab("UUID="+uuid, mountpoint, fsType, []string{"defaults", "nofail"}); err != nil {
		return fmt.Errorf("%s is mounted at %s but could not be added to /etc/fstab: %w", device, mountpoint, err)
	}
	return nil
}

// checkFormatSize refuses a partition of size bytes too small for mkfs to make
// fsType on, which would leave it unformatted.
func checkFormatSize(fsType string, size uint64) error {
	if least := btrfs.MinFormatSize(fsType); size < least {
		return fmt.Errorf("%w: %s needs a partition of at least %d MiB", bridgeipc.ErrInvalidArgs, fsType, least>>20)
	}
	return nil
}

// checkPartitionCreate validates a create request, format and mount included,
// before anything is written, and returns the disk. Partitions already on
// the disk may be in use; only the disk itself must not be.
func checkPartitionCreate(req apischema.PartitionCreateRequest, inv partitionInventory) (apischema.StorageBlockDevice, error) {
	disk, err := inv.writableDisk(req.Device)
	if err != nil {
		return disk, err
	}
	if disk.PTType != "gpt" {
		return disk, fmt.Errorf("%w: %s has no GPT partition table; initialize it first", bridgeipc.ErrInvalidArgs, disk.Path)
	}
	if err = deviceInUse(disk, inv.fss, inv.offline, false, false); err != nil {
		return disk, err
	}
	if req.SizeBytes < btrfs.MinUserPartition {
		return disk, fmt.Errorf("%w: a partition must be at least %d MiB", bridgeipc.ErrInvalidArgs, btrfs.MinUserPartition>>20)
	}
	end := req.StartBytes + req.SizeBytes // a wrap past 2^64 lands before the start
	inside := func(r apischema.StorageRegion) bool {
		return r.StartBytes <= req.StartBytes && end <= r.StartBytes+r.SizeBytes
	}
	if end < req.StartBytes || !slices.ContainsFunc(disk.FreeRegions, inside) {
		return disk, fmt.Errorf("%w: bytes %d to %d of %s are not free space", bridgeipc.ErrConflict, req.StartBytes, end, disk.Path)
	}
	if req.Format == nil {
		if req.Mount != nil {
			return disk, fmt.Errorf("%w: a new partition must be formatted to be mounted", bridgeipc.ErrInvalidArgs)
		}
		return disk, nil
	}
	if err = btrfs.ValidateFormat(req.Format.FSType, req.Format.Label); err != nil {
		return disk, fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
	}
	// The size btrfs.CreatePartition makes: the range rounded inward to whole
	// MiB, which SizeBytes of at least 1 MiB keeps from wrapping below zero.
	const mib = 1 << 20
	if err = checkFormatSize(req.Format.FSType, end/mib*mib-(req.StartBytes+mib-1)/mib*mib); err != nil {
		return disk, err
	}
	if req.Mount != nil {
		err = checkMountpoint(req.Mount.Mountpoint, req.Mount.Persist, inv.devices)
	}
	return disk, err
}

// CreatePartition adds a partition in a free region of a GPT disk and, when
// asked, formats and mounts it, reporting each step. It returns the new
// partition's path. The disk stays locked against udev throughout, so its
// partition nodes do not flap under mkfs.
func CreatePartition(ctx context.Context, req apischema.PartitionCreateRequest, report func(step string)) (string, error) {
	inv, err := loadPartitionInventory(ctx)
	if err != nil {
		return "", err
	}
	disk, err := checkPartitionCreate(req, inv)
	if err != nil {
		return "", err
	}
	unlock, err := lockDisk(ctx, disk.Path)
	if err != nil {
		return "", err
	}
	defer unlock()
	report("Partitioning")
	part, err := createPartition(ctx, disk.Path, req.StartBytes, req.SizeBytes)
	if err != nil {
		return "", conflictIfBusy(fmt.Errorf("create partition on %s: %w", disk.Path, err))
	}
	var fsType, mountpoint string
	if f := req.Format; f != nil {
		fsType = f.FSType
		report(formatStep)
		uuid, formatErr := formatDevice(ctx, part, f.FSType, f.Label, false)
		if formatErr != nil {
			return "", fmt.Errorf("partition %s was created but not formatted: %w", part, formatErr)
		}
		if m := req.Mount; m != nil {
			mountpoint = m.Mountpoint
			report("Mounting at " + m.Mountpoint)
			if mountErr := mountFilesystem(part, f.FSType, m.Mountpoint); mountErr != nil {
				return "", fmt.Errorf("partition %s was formatted but not mounted: %w", part, mountErr)
			}
			if fstabErr := persistMount(m.Persist, part, uuid, f.FSType, m.Mountpoint); fstabErr != nil {
				return "", fstabErr
			}
		}
	}
	slog.Info("created partition", "device", part, "disk", disk.Path, "sizeBytes", req.SizeBytes, "fsType", fsType, "mountpoint", mountpoint)
	return part, nil
}

// FormatPartition makes a filesystem on an unused partition, reporting the
// step; a partition with a filesystem signature needs Overwrite.
func FormatPartition(ctx context.Context, req apischema.PartitionFormatRequest, report func(step string)) error {
	inv, err := loadPartitionInventory(ctx)
	if err != nil {
		return err
	}
	part, disk, err := inv.writablePartition(req.Device)
	if err != nil {
		return err
	}
	if err = btrfs.ValidateFormat(req.FSType, req.Label); err != nil {
		return fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
	}
	if err = checkFormatSize(req.FSType, part.SizeBytes); err != nil {
		return err
	}
	if err = deviceInUse(part, inv.fss, inv.offline, false, false); err != nil {
		return err
	}
	if part.FSType != "" && !req.Overwrite {
		return fmt.Errorf("%w: %s has a %s signature; overwrite it to format", bridgeipc.ErrConflict, part.Path, part.FSType)
	}
	unlock, err := lockDisk(ctx, disk.Path)
	if err != nil {
		return err
	}
	defer unlock()
	// After the lock, which can wait out another format: the partition may
	// have been mounted or claimed meanwhile.
	if err = checkNotBusy(part.Path); err != nil {
		return conflictIfBusy(err)
	}
	report(formatStep)
	uuid, err := formatDevice(ctx, part.Path, req.FSType, req.Label, req.Overwrite)
	if err != nil {
		return conflictIfBusy(fmt.Errorf("format %s: %w", part.Path, err))
	}
	slog.Info("formatted partition", "device", part.Path, "fsType", req.FSType, "uuid", uuid)
	return nil
}

// DeletePartition removes an unused partition from its disk's GPT, leaving
// its data in place.
func DeletePartition(ctx context.Context, req apischema.BlockDeviceRequest) (apischema.SuccessResponse, error) {
	inv, err := loadPartitionInventory(ctx)
	if err != nil {
		return apischema.SuccessResponse{}, err
	}
	part, disk, err := inv.writablePartition(req.Device)
	if err != nil {
		return apischema.SuccessResponse{}, err
	}
	if disk.PTType != "gpt" {
		return apischema.SuccessResponse{}, fmt.Errorf("%w: %s is not on a GPT disk; only GPT partitions can be deleted", bridgeipc.ErrInvalidArgs, part.Path)
	}
	if err = deviceInUse(part, inv.fss, inv.offline, false, false); err != nil {
		return apischema.SuccessResponse{}, err
	}
	unlock, err := lockDiskForCall(ctx, disk.Path)
	if err != nil {
		return apischema.SuccessResponse{}, err
	}
	defer unlock()
	if err = deletePartition(ctx, disk.Path, part.PartNumber); err != nil {
		return apischema.SuccessResponse{}, conflictIfBusy(fmt.Errorf("delete %s: %w", part.Path, err))
	}
	slog.Info("deleted partition", "device", part.Path, "disk", disk.Path, "number", part.PartNumber)
	return apischema.SuccessResponse{Success: true}, nil
}

// notMountable are signatures with nothing to mount: swap, and containers for
// other layers.
var notMountable = []string{"swap", "LVM2_member", "linux_raid_member", "crypto_LUKS", "zfs_member"}

// MountPartition mounts a filesystem that is not in use and, with Persist,
// records it in /etc/fstab.
func MountPartition(ctx context.Context, req apischema.PartitionMountRequest) (apischema.StorageMountResult, error) {
	inv, err := loadPartitionInventory(ctx)
	if err != nil {
		return apischema.StorageMountResult{}, err
	}
	dev, err := inv.device(req.Device)
	if err != nil {
		return apischema.StorageMountResult{}, err
	}
	switch {
	case dev.Type != "part":
		return apischema.StorageMountResult{}, fmt.Errorf("%w: %s is not a partition", bridgeipc.ErrInvalidArgs, dev.Path)
	case dev.FSType == "":
		return apischema.StorageMountResult{}, fmt.Errorf("%w: %s has no filesystem", bridgeipc.ErrInvalidArgs, dev.Path)
	case slices.Contains(notMountable, dev.FSType):
		return apischema.StorageMountResult{}, fmt.Errorf("%w: %s holds %s, which cannot be mounted", bridgeipc.ErrInvalidArgs, dev.Path, dev.FSType)
	}
	if err = deviceInUse(dev, inv.fss, inv.offline, true, req.Persist); err != nil {
		return apischema.StorageMountResult{}, err
	}
	if req.Persist && dev.UUID == "" {
		return apischema.StorageMountResult{}, fmt.Errorf("%w: %s has no filesystem UUID to record in /etc/fstab", bridgeipc.ErrInvalidArgs, dev.Path)
	}
	if err = checkMountpoint(req.Mountpoint, req.Persist, inv.devices); err != nil {
		return apischema.StorageMountResult{}, err
	}
	if err = checkNotBusy(dev.Path); err != nil {
		return apischema.StorageMountResult{}, conflictIfBusy(err)
	}
	if err = mountFilesystem(dev.Path, dev.FSType, req.Mountpoint); err != nil {
		return apischema.StorageMountResult{}, conflictIfBusy(err)
	}
	if err = persistMount(req.Persist, dev.Path, dev.UUID, dev.FSType, req.Mountpoint); err != nil {
		return apischema.StorageMountResult{}, err
	}
	slog.Info("mounted filesystem", "device", dev.Path, "mountpoint", req.Mountpoint, "persist", req.Persist)
	return mountResult(req.Mountpoint, ""), nil
}

// InitializeDisk replaces everything on a disk, which with every partition
// on it must be unused, with an empty GPT.
func InitializeDisk(ctx context.Context, req apischema.BlockDeviceRequest) (apischema.SuccessResponse, error) {
	inv, err := loadPartitionInventory(ctx)
	if err != nil {
		return apischema.SuccessResponse{}, err
	}
	disk, err := inv.writableDisk(req.Device)
	if err != nil {
		return apischema.SuccessResponse{}, err
	}
	for _, d := range inv.devices {
		if d.Path == disk.Path || (d.Type == "part" && slices.Contains(d.Parents, disk.Path)) {
			if err = deviceInUse(d, inv.fss, inv.offline, false, false); err != nil {
				return apischema.SuccessResponse{}, err
			}
		}
	}
	unlock, err := lockDiskForCall(ctx, disk.Path)
	if err != nil {
		return apischema.SuccessResponse{}, err
	}
	defer unlock()
	if err = initializeDisk(ctx, disk.Path); err != nil {
		return apischema.SuccessResponse{}, conflictIfBusy(fmt.Errorf("initialize %s: %w", disk.Path, err))
	}
	slog.Info("initialized disk", "device", disk.Path)
	return apischema.SuccessResponse{Success: true}, nil
}
