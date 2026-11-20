package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
	scanOfflinePools    = btrfs.FindOfflinePools
	createPartition     = btrfs.CreatePartition
	deletePartition     = btrfs.DeletePartition
	initializeDisk      = btrfs.InitializeDisk
	formatDevice        = btrfs.Format
	checkNotBusy        = btrfs.CheckNotBusy
	readMemberSignature = btrfs.ReadMemberSignature
	mountDevice         = unix.Mount
	lockDisk            = btrfs.LockDisk
	partitionDisk       = btrfs.PartitionDisk
	addPartition        = btrfs.AddPartition
	createBtrfsPool     = btrfs.CreatePool
	mountBtrfsPool      = btrfs.MountPool
	mountOfflinePool    = btrfs.MountOfflinePool
	wipePoolSuperblocks = btrfs.WipePoolSuperblocks
	partitionNumber     = btrfs.PartitionNumber
	unmountPool         = unix.Unmount
	addPoolFstab        = addToFstab
	findBtrfsFilesystem = btrfs.FindFilesystem
)

// askUdevToProbe has udev probe device again, as `udevadm trigger` does, by
// writing "change" to its uevent file in sysfs; a failure is logged. A var
// so tests can stub it.
var askUdevToProbe = func(device string) {
	f, err := os.OpenFile(filepath.Join(sysClassBlockRoot, filepath.Base(device), "uevent"), os.O_WRONLY, 0)
	if err == nil {
		_, err = f.WriteString("change")
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		slog.Warn("udev probe request failed", "device", device, "error", err)
	}
}

// formatProgress turns Format's trim progress into steps: the trim, with
// its percent done, then the format. The trim takes minutes on a large SSD,
// which is why create and format run as tasks.
func formatProgress(report func(step string, pct float64)) func(float64) {
	return func(pct float64) {
		if pct < 100 {
			report(fmt.Sprintf("Trimming before formatting: %.0f%% complete", pct), pct)
			return
		}
		report("Formatting", 0)
	}
}

// callLockWait bounds how long a call waits for the disk lock: 5 s covers
// udevd's brief shared lock while it probes, without hanging the request
// behind a long mkfs. A var so tests can shorten it.
var callLockWait = 5 * time.Second

func partitionBindings() apischema.BindingSet {
	return apischema.Bindings(
		apischema.MutationTask("storage.partition_create", runPartitionCreateTask, apischema.Privileged(), apischema.Progress[BtrfsOpProgress](), apischema.WithTaskMetadata(func(req apischema.PartitionCreateRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: []string{req.Device, "partition"}, Label: "Creating partition", Device: req.Device}
		})),
		apischema.MutationTask("storage.partition_format", runPartitionFormatTask, apischema.Privileged(), apischema.Progress[BtrfsOpProgress](), apischema.WithTaskMetadata(func(req apischema.PartitionFormatRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: []string{req.Device, "format"}, Label: "Formatting partition", Device: req.Device}
		})),
	)
}

func runPartitionCreateTask(ctx context.Context, task *bridgeipc.Task, req apischema.PartitionCreateRequest) (apischema.SuccessPathResponse, error) {
	part, err := CreatePartition(ctx, req, func(step string, pct float64) {
		task.ReportProgress(BtrfsOpProgress{Op: "partition", Percent: pct, Message: step})
	})
	if err != nil {
		return apischema.SuccessPathResponse{}, err
	}
	return apischema.SuccessPathResponse{Success: true, Path: part}, nil
}

func runPartitionFormatTask(ctx context.Context, task *bridgeipc.Task, req apischema.PartitionFormatRequest) (apischema.SuccessResponse, error) {
	err := FormatPartition(ctx, req, func(step string, pct float64) {
		task.ReportProgress(BtrfsOpProgress{Op: "format", Percent: pct, Message: step})
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
// the kernel's partition table is older than the disk's, a disk to
// partition whole has partitions, or the devices of an unmounted pool are
// not all disks it can tell are the pool's.
func conflictIfBusy(err error) error {
	if errors.Is(err, unix.EBUSY) || errors.Is(err, btrfs.ErrDeviceHasData) || errors.Is(err, btrfs.ErrStaleTable) || errors.Is(err, btrfs.ErrHasPartitions) || errors.Is(err, btrfs.ErrUnclearDisks) {
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
// asked, formats and mounts it, reporting each step and, while it trims, the
// percent done. It returns the new partition's path. The disk stays locked
// against udev throughout, so its partition nodes do not flap under mkfs.
func CreatePartition(ctx context.Context, req apischema.PartitionCreateRequest, report func(step string, pct float64)) (string, error) {
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
	report("Partitioning", 0)
	part, err := createPartition(ctx, disk.Path, req.StartBytes, req.SizeBytes)
	if err != nil {
		return "", conflictIfBusy(fmt.Errorf("create partition on %s: %w", disk.Path, err))
	}
	var fsType, mountpoint string
	if f := req.Format; f != nil {
		fsType = f.FSType
		uuid, formatErr := formatDevice(ctx, part, f.FSType, f.Label, false, formatProgress(report))
		if formatErr != nil {
			return "", fmt.Errorf("partition %s was created but not formatted: %w", part, formatErr)
		}
		if m := req.Mount; m != nil {
			mountpoint = m.Mountpoint
			report("Mounting at "+m.Mountpoint, 0)
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
// steps; a partition with a filesystem signature needs Overwrite, and one
// that is a member of an md array, an LVM volume group or another set also
// needs the request to confirm that member.
func FormatPartition(ctx context.Context, req apischema.PartitionFormatRequest, report func(step string, pct float64)) error {
	part, disk, err := checkFormat(ctx, req, btrfs.MemberSignature{})
	if err != nil {
		return err
	}
	unlock, err := lockDisk(ctx, disk.Path)
	if err != nil {
		return err
	}
	var reprobe string
	defer func() { unlockThenProbe(unlock, reprobe) }()
	// Again under the lock, which can wait out another format and keeps
	// udev from probing the disk meanwhile: the partition may have been
	// mounted, claimed or made a member since, with a signature udev has not
	// reported. The read opens it exclusively, so it is the busy check too.
	sig, err := readSignature(part.Path)
	if err != nil {
		return err
	}
	if part, _, err = checkFormat(ctx, req, sig); err != nil {
		if sig.Kind != "" {
			reprobe = sig.Device
		}
		return err
	}
	uuid, err := formatDevice(ctx, part.Path, req.FSType, req.Label, req.Overwrite, formatProgress(report))
	if err != nil {
		return conflictIfBusy(fmt.Errorf("format %s: %w", part.Path, err))
	}
	slog.Info("formatted partition", "device", part.Path, "fsType", req.FSType, "uuid", uuid)
	return nil
}

// checkFormat checks a format request against a fresh inventory, with the
// partition's membership as sig, its own signature, shows it (see
// withSignatures), and returns the partition and its disk.
func checkFormat(ctx context.Context, req apischema.PartitionFormatRequest, sig btrfs.MemberSignature) (part, disk apischema.StorageBlockDevice, err error) {
	inv, err := loadPartitionInventory(ctx)
	if err != nil {
		return part, disk, err
	}
	if part, disk, err = inv.writablePartition(req.Device); err != nil {
		return part, disk, err
	}
	if err = btrfs.ValidateFormat(req.FSType, req.Label); err != nil {
		return part, disk, fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
	}
	if err = checkFormatSize(req.FSType, part.SizeBytes); err != nil {
		return part, disk, err
	}
	if err = deviceInUse(part, inv.fss, inv.offline, false, false); err != nil {
		return part, disk, err
	}
	if part.FSType != "" && !req.Overwrite {
		return part, disk, fmt.Errorf("%w: %s has a %s signature; overwrite it to format", bridgeipc.ErrConflict, part.Path, part.FSType)
	}
	_, err = confirmSignatures([]apischema.StorageBlockDevice{part}, []btrfs.MemberSignature{sig}, req.ConfirmMembers, "erasing it to format "+part.Path)
	return part, disk, err
}

// DeletePartition removes an unused partition from its disk's GPT, leaving
// its data in place. A member of an md array, an LVM volume group or another
// set is deleted only when the request confirms it.
func DeletePartition(ctx context.Context, req apischema.PartitionDeleteRequest) (apischema.SuccessResponse, error) {
	part, disk, err := checkDelete(ctx, req, btrfs.MemberSignature{})
	if err != nil {
		return apischema.SuccessResponse{}, err
	}
	unlock, err := lockDiskForCall(ctx, disk.Path)
	if err != nil {
		return apischema.SuccessResponse{}, err
	}
	var reprobe string
	defer func() { unlockThenProbe(unlock, reprobe) }()
	// Again under the lock, with the partition's own signature (see
	// FormatPartition).
	sig, err := readSignature(part.Path)
	if err != nil {
		return apischema.SuccessResponse{}, err
	}
	if part, _, err = checkDelete(ctx, req, sig); err != nil {
		if sig.Kind != "" {
			reprobe = sig.Device
		}
		return apischema.SuccessResponse{}, err
	}
	if err = deletePartition(ctx, disk.Path, part.PartNumber); err != nil {
		return apischema.SuccessResponse{}, conflictIfBusy(fmt.Errorf("delete %s: %w", part.Path, err))
	}
	slog.Info("deleted partition", "device", part.Path, "disk", disk.Path, "number", part.PartNumber)
	return apischema.SuccessResponse{Success: true}, nil
}

// checkDelete checks a delete request against a fresh inventory, with the
// partition's membership as sig, its own signature, shows it (see
// withSignatures), and returns the partition and its disk.
func checkDelete(ctx context.Context, req apischema.PartitionDeleteRequest, sig btrfs.MemberSignature) (part, disk apischema.StorageBlockDevice, err error) {
	inv, err := loadPartitionInventory(ctx)
	if err != nil {
		return part, disk, err
	}
	if part, disk, err = inv.writablePartition(req.Device); err != nil {
		return part, disk, err
	}
	if disk.PTType != "gpt" {
		return part, disk, fmt.Errorf("%w: %s is not on a GPT disk; only GPT partitions can be deleted", bridgeipc.ErrInvalidArgs, part.Path)
	}
	if err = deviceInUse(part, inv.fss, inv.offline, false, false); err != nil {
		return part, disk, err
	}
	_, err = confirmSignatures([]apischema.StorageBlockDevice{part}, []btrfs.MemberSignature{sig}, req.ConfirmMembers, "deleting it")
	return part, disk, err
}

// readSignature is readMemberSignature for a request: a device claimed
// since the inventory is a conflict, one removed since is not found.
func readSignature(device string) (btrfs.MemberSignature, error) {
	sig, err := readMemberSignature(device)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ENXIO) {
		return sig, fmt.Errorf("%w: block device %s", bridgeipc.ErrNotFound, device)
	}
	return sig, conflictIfBusy(err)
}

// unlockThenProbe releases the disk lock, then, when device is set, asks
// udev to probe it. Format and Delete set it when they were refused under
// the lock while the device read found a member signature: the dialog
// confirms only what udev reports, udev does not probe a disk while its
// lock is held, and the read, read-only, makes it probe nothing. Initialize
// needs none: closing its writable open of the disk makes udev probe it.
func unlockThenProbe(unlock func(), device string) {
	unlock()
	if device != "" {
		askUdevToProbe(device)
	}
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

// confirmedMembers returns the member signatures among devs, which are
// already known not to be in use, refusing when confirmed does not list
// exactly those: a member nobody confirmed, or a confirmation of a member
// that is gone, which means the device changed since the user looked at it.
// what says what the confirmation is for, such as "erasing it to format
// /dev/sdb1". Nothing holds a member of a stopped md array or an inactive
// LVM volume group, so deviceInUse lets it through; a member of a btrfs pool
// the bridge knows, mounted or not, never gets this far. A btrfs signature
// that reaches the confirmation belongs to no pool the bridge can read, so
// it cannot be destroyed as one and is erased only when named.
func confirmedMembers(devs []apischema.StorageBlockDevice, confirmed []apischema.DiskMemberConfirmed, what string) ([]apischema.DiskMemberConfirmed, error) {
	var members []apischema.DiskMemberConfirmed
	for _, d := range devs {
		m := memberOf(d)
		if m == nil {
			continue
		}
		member := apischema.DiskMemberConfirmed{Device: d.Path, Kind: m.Kind, UUID: m.UUID}
		if !slices.Contains(confirmed, member) {
			return nil, fmt.Errorf("%w: %s: Member of %s; confirm %s", bridgeipc.ErrConflict, d.Path, describeMember(*m), what)
		}
		members = append(members, member)
	}
	for _, c := range confirmed {
		if !slices.Contains(members, c) {
			return nil, fmt.Errorf("%w: %s is no longer the %s member that was confirmed; review the disk again", bridgeipc.ErrConflict, c.Device, c.Kind)
		}
	}
	return members, nil
}

// confirmSignatures is confirmedMembers for devs with the memberships found
// on the devices themselves (see withSignatures).
func confirmSignatures(devs []apischema.StorageBlockDevice, found []btrfs.MemberSignature, confirmed []apischema.DiskMemberConfirmed, what string) ([]apischema.DiskMemberConfirmed, error) {
	devs, err := withSignatures(devs, found)
	if err != nil {
		return nil, err
	}
	return confirmedMembers(devs, confirmed, what)
}

// withSignatures returns devs with the md and LVM2 memberships found on the
// devices themselves (btrfs.ReadMemberSignature), which udev may not have
// reported: it does not probe a disk while the disk lock is held, nor before
// it gets to a change. A membership found is added where udev reported none,
// and replaces an md, LVM or btrfs membership udev reported whenever its
// kind or UUID differs: libblkid reads md before LVM2 and both before any
// filesystem, as the read does, so the read is what udev will report once it
// probes, and a confirmation of the old one no longer holds. Only a
// membership of another set (kind "other") stays with its own confirmation:
// it is a container the read cannot see, such as a firmware RAID holding an
// LVM2 label, which libblkid reports first. One udev reported that the read
// does not find stays too, so it is still named and confirmed. A signature
// on a device devs does not list is refused.
func withSignatures(devs []apischema.StorageBlockDevice, found []btrfs.MemberSignature) ([]apischema.StorageBlockDevice, error) {
	devs = slices.Clone(devs)
	for _, s := range found {
		if s.Kind == "" {
			continue
		}
		i := slices.IndexFunc(devs, func(d apischema.StorageBlockDevice) bool { return d.Path == s.Device })
		if i < 0 {
			return nil, fmt.Errorf("%w: %s appeared after the disk was read; review the disk again", bridgeipc.ErrConflict, s.Device)
		}
		if m := memberOf(devs[i]); m == nil || m.Kind != "other" && (m.Kind != s.Kind || m.UUID != s.UUID) {
			devs[i].Member = &apischema.StorageMember{Kind: s.Kind, UUID: s.UUID}
		}
	}
	return devs, nil
}

// checkInitialize checks an initialize request against a fresh inventory:
// the disk and every partition of it must be unused, and the members among
// them exactly those confirmed. It returns the disk and those devices.
func checkInitialize(ctx context.Context, req apischema.DiskInitializeRequest) (disk apischema.StorageBlockDevice, devs []apischema.StorageBlockDevice, err error) {
	inv, err := loadPartitionInventory(ctx)
	if err != nil {
		return disk, nil, err
	}
	if disk, err = inv.writableDisk(req.Device); err != nil {
		return disk, nil, err
	}
	for _, d := range inv.devices {
		if d.Path != disk.Path && (d.Type != "part" || !slices.Contains(d.Parents, disk.Path)) {
			continue
		}
		if err = deviceInUse(d, inv.fss, inv.offline, false, false); err != nil {
			return disk, nil, err
		}
		devs = append(devs, d)
	}
	_, err = confirmedMembers(devs, req.ConfirmMembers, "erasing it to initialize "+disk.Path)
	return disk, devs, err
}

// InitializeDisk replaces everything on a disk, which with every partition
// on it must be unused, with an empty GPT. A member of an md array or an LVM
// volume group on it, which nothing holds once the array is stopped or the
// group inactive, is erased only when the request confirms exactly the
// members found; a member of a btrfs pool is refused like any device in use.
func InitializeDisk(ctx context.Context, req apischema.DiskInitializeRequest) (apischema.SuccessResponse, error) {
	disk, _, err := checkInitialize(ctx, req)
	if err != nil {
		return apischema.SuccessResponse{}, err
	}
	unlock, err := lockDiskForCall(ctx, disk.Path)
	if err != nil {
		return apischema.SuccessResponse{}, err
	}
	defer unlock()
	// Again under the lock, then against the signatures btrfs.InitializeDisk
	// reads under its exclusive claim on the disk before it changes anything.
	disk, devs, err := checkInitialize(ctx, req)
	if err != nil {
		return apischema.SuccessResponse{}, err
	}
	what := "erasing it to initialize " + disk.Path
	var members []apischema.DiskMemberConfirmed
	err = initializeDisk(ctx, disk.Path, func(found []btrfs.MemberSignature) (err error) {
		members, err = confirmSignatures(devs, found, req.ConfirmMembers, what)
		return err
	})
	if errors.Is(err, os.ErrNotExist) {
		// A partition node went while the disk was read: it changed under us.
		return apischema.SuccessResponse{}, fmt.Errorf("%w: initialize %s, which changed while it was read; review the disk again: %w", bridgeipc.ErrConflict, disk.Path, err)
	}
	if err != nil {
		return apischema.SuccessResponse{}, conflictIfBusy(fmt.Errorf("initialize %s: %w", disk.Path, err))
	}
	slog.Info("initialized disk", "device", disk.Path, "erasedMembers", len(members))
	return apischema.SuccessResponse{Success: true}, nil
}
