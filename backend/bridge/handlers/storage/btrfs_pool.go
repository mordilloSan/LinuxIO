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

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

// btrfsPoolFstabOptions are the mount options of a pool LinuxIO records in
// /etc/fstab; nofail keeps a pool with a missing device from blocking boot.
var btrfsPoolFstabOptions = []string{"defaults", "noatime", "nofail"}

// CreateBtrfsPool validates the request, formats the member devices, mounts the
// new pool and records it in /etc/fstab.
func CreateBtrfsPool(ctx context.Context, req apischema.BtrfsPoolCreateRequest) (btrfs.BtrfsFilesystem, error) {
	devices, err := ListBlockDevices(ctx)
	if err != nil {
		return btrfs.BtrfsFilesystem{}, err
	}
	filesystems, err := btrfs.ListFilesystems(ctx)
	if err != nil {
		return btrfs.BtrfsFilesystem{}, err
	}
	if err = checkPoolCreate(req, devices, filesystems, getFstabEntries()); err != nil {
		return btrfs.BtrfsFilesystem{}, err
	}
	// Mounting over files would hide them.
	if entries, readErr := os.ReadDir(req.Mountpoint); readErr == nil && len(entries) > 0 {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("mountpoint %s is not empty", req.Mountpoint)
	}
	slog.Info("creating btrfs pool", "label", req.Label, "devices", req.Devices, "data", req.DataProfile, "metadata", req.MetadataProfile, "mountpoint", req.Mountpoint, "force", req.Force, "partition", req.Partition)
	members := req.Devices
	if req.Partition {
		members = make([]string, 0, len(req.Devices))
		for _, d := range req.Devices {
			part, partErr := btrfs.PartitionDisk(ctx, d)
			if partErr != nil {
				return btrfs.BtrfsFilesystem{}, fmt.Errorf("partition %s for btrfs pool: %w", d, partErr)
			}
			members = append(members, part)
		}
	}
	uuid, err := btrfs.CreatePool(ctx, req.Label, req.DataProfile, req.MetadataProfile, members, req.Force)
	if err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("create btrfs pool %q: %w", req.Label, err)
	}
	if err := btrfs.MountPool(members[0], req.Mountpoint, false); err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("pool %s was created but not mounted: %w", uuid, err)
	}
	if err := addToFstab("UUID="+uuid, req.Mountpoint, "btrfs", btrfsPoolFstabOptions); err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("pool %s is mounted but could not be added to /etc/fstab: %w", uuid, err)
	}
	slog.Info("created btrfs pool", "uuid", uuid, "mountpoint", req.Mountpoint)
	return btrfs.FindFilesystem(ctx, uuid)
}

// checkPoolCreate validates a create request against the host: label,
// profiles for the device count, every device free, and an unused
// mountpoint outside the system directories.
func checkPoolCreate(req apischema.BtrfsPoolCreateRequest, devices []apischema.StorageBlockDevice, filesystems []btrfs.BtrfsFilesystem, fstab map[string]fstabEntry) error {
	if err := btrfs.ValidateLabel(req.Label); err != nil {
		return err
	}
	if len(req.Devices) == 0 {
		return errors.New("select at least one device")
	}
	for i, d := range req.Devices {
		if slices.Contains(req.Devices[:i], d) {
			return fmt.Errorf("device %s listed twice", d)
		}
		if err := checkBtrfsDeviceFree(devices, filesystems, d, req.Force); err != nil {
			return err
		}
	}
	data, meta := req.DataProfile, req.MetadataProfile
	if err := btrfs.ValidateProfiles(&data, &meta, len(req.Devices)); err != nil {
		return err
	}
	return checkPoolMountpoint(req.Mountpoint, devices, fstab)
}

func checkPoolMountpoint(mountpoint string, devices []apischema.StorageBlockDevice, fstab map[string]fstabEntry) error {
	if !validPath.MatchString(mountpoint) || filepath.Clean(mountpoint) != mountpoint {
		return fmt.Errorf("invalid mountpoint %q", mountpoint)
	}
	if isSystemPath(mountpoint) || isProtectedMount(mountpoint) {
		return fmt.Errorf("mountpoint %s is a system directory", mountpoint)
	}
	if _, ok := fstab[mountpoint]; ok {
		return fmt.Errorf("mountpoint %s is already in /etc/fstab", mountpoint)
	}
	for _, d := range devices {
		if slices.Contains(d.Mountpoints, mountpoint) {
			return fmt.Errorf("mountpoint %s is in use by %s", mountpoint, d.Path)
		}
	}
	return nil
}

// DestroyBtrfsPool unmounts the pool, drops its fstab entries and wipes its
// devices.
func DestroyBtrfsPool(ctx context.Context, uuid string) error {
	devices, err := poolDevicesForDestroy(ctx, uuid)
	if err != nil {
		return fmt.Errorf("destroy btrfs pool %s: %w", uuid, err)
	}
	slog.Info("destroying btrfs pool", "uuid", uuid, "devices", devices)
	for mountpoint, entry := range getFstabEntries() {
		if strings.EqualFold(entry.source, "UUID="+uuid) {
			if err := removeFromFstab(mountpoint); err != nil {
				return fmt.Errorf("remove %s from /etc/fstab: %w", mountpoint, err)
			}
		}
	}
	for _, d := range devices {
		if err := btrfs.WipeDevice(d); err != nil {
			return fmt.Errorf("wipe %s of btrfs pool %s: %w", d, uuid, err)
		}
	}
	slog.Info("destroyed btrfs pool", "uuid", uuid)
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
// that holds a protected system mount.
func poolMountpoints(fs btrfs.BtrfsFilesystem) ([]string, error) {
	var mountpoints []string
	for _, m := range fs.Mountpoints {
		if isProtectedMount(m.Mountpoint) {
			return nil, fmt.Errorf("refusing to destroy a pool mounted at %s", m.Mountpoint)
		}
		if !slices.Contains(mountpoints, m.Mountpoint) {
			mountpoints = append(mountpoints, m.Mountpoint)
		}
	}
	sort.SliceStable(mountpoints, func(i, j int) bool { return len(mountpoints[i]) > len(mountpoints[j]) })
	return mountpoints, nil
}

// MountBtrfsPool mounts an unmounted pool, adding an fstab entry when it has
// none.
func MountBtrfsPool(ctx context.Context, req apischema.BtrfsPoolMountRequest) (btrfs.BtrfsFilesystem, error) {
	pool, err := findOfflinePool(ctx, req.UUID)
	if err != nil {
		return btrfs.BtrfsFilesystem{}, err
	}
	if len(pool.Devices) < pool.NumDevices && !req.Degraded {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("pool %s is missing %d of %d devices; mount it degraded to use it anyway", pool.UUID, pool.NumDevices-len(pool.Devices), pool.NumDevices)
	}
	for _, d := range pool.Devices {
		if err := btrfs.RegisterDevice(d); err != nil {
			return btrfs.BtrfsFilesystem{}, err
		}
	}
	mountpoint := pool.Mountpoint
	persist := mountpoint == ""
	if persist {
		devices, err := ListBlockDevices(ctx)
		if err != nil {
			return btrfs.BtrfsFilesystem{}, err
		}
		mountpoint = defaultPoolMountpoint(pool)
		if err := checkPoolMountpoint(mountpoint, devices, getFstabEntries()); err != nil {
			return btrfs.BtrfsFilesystem{}, err
		}
	}
	slog.Info("mounting btrfs pool", "uuid", pool.UUID, "mountpoint", mountpoint, "degraded", req.Degraded)
	if err := btrfs.MountPool(pool.Devices[0], mountpoint, req.Degraded); err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("mount btrfs pool %s at %s: %w", pool.UUID, mountpoint, err)
	}
	if persist {
		if err := addToFstab("UUID="+pool.UUID, mountpoint, "btrfs", btrfsPoolFstabOptions); err != nil {
			return btrfs.BtrfsFilesystem{}, fmt.Errorf("pool %s is mounted but could not be added to /etc/fstab: %w", pool.UUID, err)
		}
	}
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
	slog.Info("growing btrfs device", "uuid", req.UUID, "device", req.DeviceID)
	if err := btrfs.GrowDevice(ctx, req.UUID, req.DeviceID); err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("grow btrfs device %d of %s: %w", req.DeviceID, req.UUID, err)
	}
	return btrfs.FindFilesystem(ctx, req.UUID)
}

// SetBtrfsLabel relabels a btrfs filesystem.
func SetBtrfsLabel(ctx context.Context, req apischema.BtrfsLabelRequest) (btrfs.BtrfsFilesystem, error) {
	slog.Info("relabeling btrfs filesystem", "uuid", req.UUID, "label", req.Label)
	if err := btrfs.SetLabel(ctx, req.UUID, req.Label); err != nil {
		return btrfs.BtrfsFilesystem{}, fmt.Errorf("relabel btrfs filesystem %s to %q: %w", req.UUID, req.Label, err)
	}
	return btrfs.FindFilesystem(ctx, req.UUID)
}

func findOfflinePool(ctx context.Context, uuid string) (btrfs.BtrfsOfflinePool, error) {
	pools, err := ListOfflinePools(ctx)
	if err != nil {
		return btrfs.BtrfsOfflinePool{}, err
	}
	i := slices.IndexFunc(pools, func(p btrfs.BtrfsOfflinePool) bool { return p.UUID == uuid })
	if i < 0 {
		return btrfs.BtrfsOfflinePool{}, fmt.Errorf("unmounted btrfs filesystem %s not found", uuid)
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
