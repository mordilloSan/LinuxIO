package app

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/disk"

	commonutils "github.com/mordilloSan/LinuxIO/backend/common/utils"
	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

type cachedLiveFilesystem struct {
	info monitoringapi.FilesystemInfo
	at   time.Time
}

var (
	livePartitions = disk.PartitionsWithContext
	liveDiskUsage  = disk.UsageWithContext
)

// filesystemInfo discovers mounts on every uncached live sample while reusing
// usage values for sleeping non-root filesystems. The mountpoint is the cache
// key: several mounts can share a device and mounts can appear or disappear.
func (a *App) filesystemInfo(ctx context.Context) []monitoringapi.FilesystemInfo {
	if a.fsManager == nil {
		return []monitoringapi.FilesystemInfo{}
	}
	partitions, err := livePartitions(ctx, true)
	if err != nil {
		return []monitoringapi.FilesystemInfo{}
	}

	a.Lock()
	defer a.Unlock()
	if a.fsManager.liveFilesystemCache == nil {
		a.fsManager.liveFilesystemCache = make(map[string]cachedLiveFilesystem)
	}
	now := time.Now()
	items := make([]monitoringapi.FilesystemInfo, 0, len(partitions))
	seenMounts := make(map[string]struct{}, len(partitions))
	for _, partition := range partitions {
		if err := ctx.Err(); err != nil {
			return items
		}
		if isPseudoFS(partition) {
			continue
		}
		mountpoint := partition.Mountpoint
		seenMounts[mountpoint] = struct{}{}
		cached, found := a.fsManager.liveFilesystemCache[mountpoint]
		root := mountpoint == "/"
		tracked := trackedFilesystemStats(a.fsManager.fsStats, mountpoint)
		trackedMatches := tracked != nil && tracked.Device == partition.Device && tracked.FSType == partition.Fstype
		if trackedMatches && !tracked.UsageAt.IsZero() && now.Sub(tracked.UsageAt) < liveReuseWindow {
			info := filesystemInfoFromTracked(tracked, partition)
			a.fsManager.liveFilesystemCache[mountpoint] = cachedLiveFilesystem{info: info, at: tracked.UsageAt}
			items = append(items, info)
			continue
		}
		if info, ok := reusableCachedFilesystem(cached, found, partition, root, a.fsManager.diskUsageCacheDuration, now); ok {
			items = append(items, info)
			continue
		}

		usage, err := liveDiskUsage(ctx, mountpoint)
		if err != nil {
			// A mount may disappear between partition discovery and usage. Keep
			// serving the other mounts in this optional live section.
			continue
		}
		info := filesystemInfoFromUsage(partition, usage)
		a.fsManager.liveFilesystemCache[mountpoint] = cachedLiveFilesystem{info: info, at: now}
		items = append(items, info)
	}
	for mountpoint := range a.fsManager.liveFilesystemCache {
		if _, seen := seenMounts[mountpoint]; !seen {
			delete(a.fsManager.liveFilesystemCache, mountpoint)
		}
	}
	slices.SortFunc(items, func(a, b monitoringapi.FilesystemInfo) int {
		return cmp.Or(strings.Compare(a.Mountpoint, b.Mountpoint), strings.Compare(a.Device, b.Device))
	})
	return items
}

func reusableCachedFilesystem(cached cachedLiveFilesystem, found bool, partition disk.PartitionStat, root bool, duration time.Duration, now time.Time) (monitoringapi.FilesystemInfo, bool) {
	if !found || root || duration <= 0 || cached.info.Device != partition.Device || cached.info.FSType != partition.Fstype || cached.at.IsZero() || now.Before(cached.at) || now.Sub(cached.at) >= duration {
		return monitoringapi.FilesystemInfo{}, false
	}
	info := cached.info
	readOnly := hasReadOnlyOpt(partition.Opts)
	info.ReadOnly = &readOnly
	return info, true
}

func trackedFilesystemStats(stats map[string]*system.FsStats, mountpoint string) *system.FsStats {
	for _, value := range stats {
		if value != nil && value.Mountpoint == mountpoint {
			return value
		}
	}
	return nil
}

func filesystemInfoFromTracked(stats *system.FsStats, partition disk.PartitionStat) monitoringapi.FilesystemInfo {
	readOnly := hasReadOnlyOpt(partition.Opts)
	return monitoringapi.FilesystemInfo{Device: partition.Device, Mountpoint: partition.Mountpoint, FSType: partition.Fstype, ReadOnly: &readOnly, Total: stats.TotalBytes, Used: stats.UsedBytes, Free: stats.FreeBytes, UsedPercent: stats.UsedPercent, InodesTotal: commonutils.OptionalUint64(stats.InodesTotal), InodesUsed: commonutils.OptionalUint64(stats.InodesUsed), InodesFree: commonutils.OptionalUint64(stats.InodesFree), InodesUsedPercent: commonutils.OptionalFloat64(stats.InodesUsedPercent)}
}

func filesystemInfoFromUsage(partition disk.PartitionStat, usage *disk.UsageStat) monitoringapi.FilesystemInfo {
	readOnly := hasReadOnlyOpt(partition.Opts)
	return monitoringapi.FilesystemInfo{
		Device: partition.Device, Mountpoint: partition.Mountpoint, FSType: partition.Fstype, ReadOnly: &readOnly,
		Total: usage.Total, Used: usage.Used, Free: usage.Free, UsedPercent: usage.UsedPercent,
		InodesTotal: commonutils.OptionalUint64(usage.InodesTotal), InodesUsed: commonutils.OptionalUint64(usage.InodesUsed),
		InodesFree: commonutils.OptionalUint64(usage.InodesFree), InodesUsedPercent: commonutils.OptionalFloat64(usage.InodesUsedPercent),
	}
}

func isPseudoFS(partition disk.PartitionStat) bool {
	if strings.HasPrefix(partition.Device, "/dev/") {
		return false
	}
	switch partition.Fstype {
	case "proc", "sysfs", "devtmpfs", "devpts", "tmpfs", "cgroup", "cgroup2", "pstore",
		"securityfs", "debugfs", "tracefs", "configfs", "overlay", "squashfs", "ramfs",
		"bpf", "nsfs", "autofs", "fusectl":
		return true
	case "ext2", "ext3", "ext4", "xfs", "btrfs", "zfs", "f2fs", "reiserfs", "jfs", "ntfs", "vfat", "exfat":
		return false
	default:
		// Network and FUSE mounts are real filesystems even when their source
		// is not under /dev.
		return false
	}
}
