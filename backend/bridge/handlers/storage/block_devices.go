package storage

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

func GetTopology(ctx context.Context) (apischema.StorageTopologyInventory, error) {
	devices, err := ListBlockDevices(ctx)
	if err != nil {
		return apischema.StorageTopologyInventory{}, err
	}
	partitions, err := disk.PartitionsWithContext(ctx, true)
	if err != nil {
		return apischema.StorageTopologyInventory{}, fmt.Errorf("list topology mounts: %w", err)
	}
	mounts := make([]apischema.StorageMount, 0, len(partitions))
	for _, partition := range partitions {
		mounts = append(mounts, apischema.StorageMount{
			Device: partition.Device, Path: partition.Mountpoint, FSType: partition.Fstype,
			ReadOnly: slices.Contains(partition.Opts, "ro"),
		})
	}
	return apischema.StorageTopologyInventory{Devices: devices, Mounts: mounts}, nil
}

// blockDevice is a native inventory entry plus the drive-only fields that are
// not part of the topology contract.
type blockDevice struct {
	apischema.StorageBlockDevice
	devno      string
	vendor     string
	rotational bool
}

// is reports whether source names this device by its node path.
func (d *blockDevice) is(source string) bool {
	return source == d.Path || source == "/dev/"+d.KernelName
}

// readBlockDevices builds the inventory from sysfs, the udev database,
// mountinfo, swaps and fstab, without running any command.
func readBlockDevices(ctx context.Context) ([]blockDevice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(sysClassBlockRoot)
	if err != nil {
		return nil, fmt.Errorf("list block devices: %w", err)
	}
	mounts, err := readMountinfo()
	if err != nil {
		return nil, err
	}
	swaps, err := readSwaps()
	if err != nil {
		return nil, err
	}
	fstab := fstabSources()

	devices := make([]blockDevice, 0, len(entries))
	for _, entry := range entries {
		d, ok := readBlockDevice(entry.Name())
		if !ok {
			continue
		}
		d.Mountpoints = mountpointsOf(d, mounts)
		d.InFstab = slices.ContainsFunc(fstab, d.inFstab)
		d.Busy = busyReason(d, swaps)
		devices = append(devices, d)
	}
	slices.SortFunc(devices, func(a, b blockDevice) int { return strings.Compare(a.Path, b.Path) })
	return devices, nil
}

// mountpointsOf lists, sorted, where d is mounted. Anonymous-device
// filesystems (btrfs) carry major 0 in mountinfo: those match by source.
func mountpointsOf(d blockDevice, mounts []mountEntry) []string {
	points := []string{}
	for _, m := range mounts {
		if (m.devno == d.devno || strings.HasPrefix(m.devno, "0:") && d.is(m.source)) && !slices.Contains(points, m.mountpoint) {
			points = append(points, m.mountpoint)
		}
	}
	slices.Sort(points)
	return points
}

// busyReason is why d must be left alone, in priority order, or "".
func busyReason(d blockDevice, swaps []string) string {
	if len(d.Mountpoints) > 0 {
		return "Mounted at " + d.Mountpoints[0]
	}
	if slices.ContainsFunc(swaps, d.is) {
		return "Active swap"
	}
	if holders := sysEntries(d.KernelName, "holders"); len(holders) > 0 {
		return "Used by " + blockPath(holders[0])
	}
	return ""
}

// readBlockDevice describes one /sys/class/block entry; false for devices that
// are not storage inventory: loop images, RAM disks and hidden multipath
// paths. Empty drives stay, as in lsblk.
func readBlockDevice(kname string) (blockDevice, bool) {
	devno := readSysString(kname, "dev")
	major, _, _ := strings.Cut(devno, ":")
	if strings.HasPrefix(kname, "loop") || devno == "" || major == "1" || readSysString(kname, "hidden") == "1" {
		return blockDevice{}, false
	}
	sectors, err := readSysUint(kname, "size")
	if err != nil {
		return blockDevice{}, false
	}

	d := blockDevice{devno: devno, rotational: readSysString(kname, "queue/rotational") == "1"}
	d.Path, d.Name, d.KernelName = blockPath(kname), kname, kname
	if dmName := readSysString(kname, "dm/name"); dmName != "" {
		d.Name = dmName
	}
	d.SizeBytes = sectors * 512
	d.ReadOnly = readSysString(kname, "ro") == "1"
	d.Parents = []string{}

	d.Type = blockType(kname)
	if d.Type == "part" {
		number, _ := readSysUint(kname, "partition")
		start, _ := readSysUint(kname, "start")
		d.PartNumber, d.StartBytes = int(number), start*512
		// The kernel nests a partition's directory under its disk's.
		if resolved, err := filepath.EvalSymlinks(filepath.Join(sysClassBlockRoot, kname)); err == nil {
			d.Parents = append(d.Parents, blockPath(filepath.Base(filepath.Dir(resolved))))
		}
	}
	for _, slave := range sysEntries(kname, "slaves") {
		if path := blockPath(slave); !slices.Contains(d.Parents, path) {
			d.Parents = append(d.Parents, path)
		}
	}
	slices.Sort(d.Parents)

	d.applyUdev(readUdev(devno))
	return d, true
}

// blockType is lsblk's type of a device: part, lvm/crypt/mpath/dm, the md
// level, rom or disk.
func blockType(kname string) string {
	switch {
	case readSysString(kname, "partition") != "":
		return "part"
	case strings.HasPrefix(kname, "dm-"):
		uuid := readSysString(kname, "dm/uuid")
		for prefix, typ := range map[string]string{"LVM-": "lvm", "CRYPT-": "crypt", "mpath-": "mpath"} {
			if strings.HasPrefix(uuid, prefix) {
				return typ
			}
		}
		return "dm"
	case strings.HasPrefix(kname, "md"):
		if level := readSysString(kname, "md/level"); level != "" {
			return level
		}
		return "md"
	case strings.HasPrefix(kname, "sr"):
		return "rom"
	}
	return "disk"
}

// applyUdev fills what udev knows: filesystem, partition entry, and for whole
// devices (lsblk shows them nowhere else) model, serial and vendor.
func (d *blockDevice) applyUdev(props map[string]string) {
	kname := d.KernelName
	d.FSType = props["ID_FS_TYPE"]
	d.UUID = decodeUdevEnc(props["ID_FS_UUID_ENC"])
	d.Label = decodeUdevEnc(props["ID_FS_LABEL_ENC"])
	d.PTType = firstProp(props, "ID_PART_TABLE_TYPE", "ID_PART_ENTRY_SCHEME")
	d.PartUUID = props["ID_PART_ENTRY_UUID"]
	d.PartLabel = decodeUdevEnc(props["ID_PART_ENTRY_NAME"])
	d.Transport = blockTransport(kname)
	if d.Type == "part" {
		return
	}
	d.Model = strings.TrimSpace(decodeUdevEnc(props["ID_MODEL_ENC"]))
	if d.Model == "" {
		d.Model = strings.TrimSpace(props["ID_MODEL"])
	}
	if d.Model == "" {
		d.Model = readSysString(kname, "device/model")
	}
	d.Serial = strings.TrimSpace(firstProp(props, "SCSI_IDENT_SERIAL", "ID_SCSI_SERIAL", "ID_SERIAL_SHORT", "ID_SERIAL"))
	if d.Serial == "" {
		d.Serial = readSysString(kname, "device/serial")
	}
	d.vendor = readSysString(kname, "device/vendor")
}

// inFstab reports whether an fstab source names this device by UUID, PARTUUID,
// LABEL, PARTLABEL or node path (including by-id style symlinks).
func (d *blockDevice) inFstab(source string) bool {
	if strings.HasPrefix(source, "/") {
		return d.is(resolveDevSource(source))
	}
	key, value, ok := strings.Cut(source, "=")
	value = strings.Trim(value, `"'`)
	if !ok || value == "" {
		return false
	}
	switch key {
	case "UUID":
		return strings.EqualFold(d.UUID, value)
	case "PARTUUID":
		return strings.EqualFold(d.PartUUID, value)
	case "LABEL":
		return d.Label == value
	case "PARTLABEL":
		return d.PartLabel == value
	}
	return false
}

func ListBlockDevices(ctx context.Context) ([]apischema.StorageBlockDevice, error) {
	native, err := readBlockDevices(ctx)
	if err != nil {
		return nil, err
	}
	devices := make([]apischema.StorageBlockDevice, 0, len(native))
	for _, d := range native {
		devices = append(devices, d.StorageBlockDevice)
	}
	addFreeSpace(devices)
	return devices, nil
}

// sysClassBlockRoot is where the kernel describes block devices; a var so
// tests can point it at a fake tree.
var sysClassBlockRoot = "/sys/class/block"

// addFreeSpace sets FreeBytes and FreeRegions on every GPT disk. A disk whose
// sysfs entries cannot be read keeps none: it is merely not offered, so the
// topology still works.
func addFreeSpace(devices []apischema.StorageBlockDevice) {
	for i, d := range devices {
		if d.Type != "disk" || d.PTType != "gpt" {
			continue
		}
		free, regions, err := gptFreeSpace(d, devices)
		if err != nil {
			slog.Debug("read free space of gpt disk failed", "device", d.Path, "error", err)
			continue
		}
		devices[i].FreeBytes, devices[i].FreeRegions = free, regions
	}
}

// gptFreeSpace returns the size of the partition btrfs.AddPartition would add
// to disk (its largest free region) and every free region of at least
// btrfs.MinUserPartition. Only the sector size needs sysfs, so no privilege is
// needed to read the raw device.
func gptFreeSpace(disk apischema.StorageBlockDevice, devices []apischema.StorageBlockDevice) (uint64, []apischema.StorageRegion, error) {
	ss, err := readSysUint(disk.KernelName, "queue/logical_block_size")
	if err != nil {
		return 0, nil, err
	}
	if ss == 0 {
		return 0, nil, fmt.Errorf("logical block size of %s is 0", disk.KernelName)
	}
	var used [][2]uint64
	for _, p := range devices {
		if p.Type == "part" && p.SizeBytes > 0 && slices.Contains(p.Parents, disk.Path) {
			used = append(used, [2]uint64{p.StartBytes / ss, (p.StartBytes+p.SizeBytes)/ss - 1})
		}
	}
	// The GPT defaults: two header sectors and 128 entries of 128 bytes at each end.
	entrySectors, sectors := 128*128/ss, disk.SizeBytes/ss
	if sectors < 2*(2+entrySectors) {
		return 0, nil, nil
	}
	firstUsable, lastUsable := 2+entrySectors, sectors-2-entrySectors
	var free uint64
	if first, last, ok := btrfs.LargestFreeRegion(firstUsable, lastUsable, used, ss); ok {
		free = (last - first + 1) * ss
	}
	var regions []apischema.StorageRegion
	for _, r := range btrfs.FreeRegions(firstUsable, lastUsable, used, ss, btrfs.MinUserPartition) {
		regions = append(regions, apischema.StorageRegion{StartBytes: r[0] * ss, SizeBytes: (r[1] - r[0] + 1) * ss})
	}
	return free, regions, nil
}

// readSysUint reads one number from /sys/class/block/<kname>/<file>.
func readSysUint(kname, file string) (uint64, error) {
	b, err := os.ReadFile(filepath.Join(sysClassBlockRoot, kname, file))
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s of %s: %w", file, kname, err)
	}
	return n, nil
}
