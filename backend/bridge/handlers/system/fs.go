package system

import (
	"context"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	"github.com/shirou/gopsutil/v4/disk"
)

func FetchFileSystemInfo(ctx context.Context, includeAll bool) ([]apischema.FilesystemInfo, error) {
	parts, err := disk.PartitionsWithContext(ctx, true)
	if err != nil {
		return nil, err
	}
	results := make([]apischema.FilesystemInfo, 0, len(parts))
	for _, p := range parts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !includeAll && isPseudoFS(p) {
			continue
		}
		usage, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil {
			continue
		}
		results = append(results, apischema.FilesystemInfo{
			Device:            p.Device,
			Mountpoint:        p.Mountpoint,
			FSType:            p.Fstype,
			ReadOnly:          utils.BoolPtr(utils.HasReadOnlyOpt(p.Opts)),
			Total:             usage.Total,
			Used:              usage.Used,
			Free:              usage.Free,
			UsedPercent:       usage.UsedPercent,
			InodesTotal:       utils.OptionalUint64(usage.InodesTotal),
			InodesUsed:        utils.OptionalUint64(usage.InodesUsed),
			InodesFree:        utils.OptionalUint64(usage.InodesFree),
			InodesUsedPercent: utils.OptionalFloat64(usage.InodesUsedPercent),
		})
	}
	return results, nil
}

func isPseudoFS(p disk.PartitionStat) bool {
	if strings.HasPrefix(p.Device, "/dev/") {
		return false
	}
	switch p.Fstype {
	case "proc", "sysfs", "devtmpfs", "devpts",
		"tmpfs", "cgroup", "cgroup2", "pstore",
		"securityfs", "debugfs", "tracefs",
		"configfs", "overlay", "squashfs", "ramfs",
		"bpf", "nsfs", "autofs", "fusectl":
		return true
	}
	switch p.Fstype {
	case "ext2", "ext3", "ext4", "xfs", "btrfs", "zfs",
		"f2fs", "reiserfs", "jfs", "ntfs", "vfat", "exfat":
		return false
	}
	return false
}
