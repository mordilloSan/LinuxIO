package live

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	psutilDisk "github.com/shirou/gopsutil/v4/disk"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector/disk"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

type cachedFilesystem struct {
	info monitoringapi.FilesystemInfo
	at   time.Time
}

// Tracker is the collector's view of the filesystems it already samples; the
// live listing reuses its usage figures when they are fresh enough.
type Tracker interface {
	Tracked() map[string]*systemmodel.FsStats
	UsageCacheDuration() time.Duration
}

// Filesystems lists the mounts for the live payload. The mountpoint is the
// cache key: several mounts can share a device and mounts can appear or
// disappear. The zero value is ready to use; mu guards the cache and the
// statfs calls in flight.
type Filesystems struct {
	mu      sync.Mutex
	cache   map[string]cachedFilesystem
	statfss map[string]*statfsCall
}

// statfsCall is one statfs of a mount. statfs takes no deadline, and on a dead
// hard-mounted share it blocks until the server answers, so it runs on its own
// goroutine, which exits when the call returns; at most one runs per mount.
type statfsCall struct {
	done  chan struct{} // closed once usage, err and at are set
	usage *psutilDisk.UsageStat
	err   error
	at    time.Time
}

// statfsWait is how long a live collection waits for the statfs it started.
// A mount whose call is still running from an earlier collection is not
// waited for at all, so a dead mount costs its own row and nothing else.
var statfsWait = time.Second

// lateStatfsMaxAge is how old the answer of a call that outlived its wait may
// be and still be served by a later collection.
const lateStatfsMaxAge = 30 * time.Second

var (
	livePartitions = psutilDisk.PartitionsWithContext
	liveDiskUsage  = psutilDisk.UsageWithContext
)

// Info discovers mounts on every uncached live sample while reusing usage
// values for sleeping non-root filesystems. appMu is the lock that guards the
// tracker's state (the collector's app mutex); Info holds it only to read the
// tracker, never across statfs, which blocks on a dead hard-mounted share.
func (f *Filesystems) Info(ctx context.Context, appMu sync.Locker, tracker Tracker) []monitoringapi.FilesystemInfo {
	partitions, err := livePartitions(ctx, true)
	if err != nil {
		return []monitoringapi.FilesystemInfo{}
	}
	partitions = slices.DeleteFunc(partitions, isPseudoFS)

	now := time.Now()
	appMu.Lock()
	usageCache := tracker.UsageCacheDuration()
	fresh := freshTrackedFilesystems(tracker.Tracked(), partitions, now)
	appMu.Unlock()

	f.mu.Lock()
	previous := maps.Clone(f.cache)
	f.mu.Unlock()
	cache := make(map[string]cachedFilesystem, len(partitions))
	items := make([]monitoringapi.FilesystemInfo, 0, len(partitions))
	for _, partition := range partitions {
		if err := ctx.Err(); err != nil {
			return items
		}
		mountpoint := partition.Mountpoint
		cached, found := previous[mountpoint]
		if entry, ok := fresh[mountpoint]; ok {
			cache[mountpoint] = entry
			items = append(items, entry.info)
			continue
		}
		if info, ok := reusableCachedFilesystem(cached, found, partition, mountpoint == "/", usageCache, now); ok {
			cache[mountpoint] = cached
			items = append(items, info)
			continue
		}

		usage, err := f.usage(ctx, mountpoint)
		if err != nil {
			// A mount may disappear between partition discovery and usage, or
			// not answer in time. Keep serving the other mounts in this
			// optional live section.
			if found {
				cache[mountpoint] = cached
			}
			continue
		}
		info := filesystemInfoFromUsage(partition, usage)
		cache[mountpoint] = cachedFilesystem{info: info, at: time.Now()}
		items = append(items, info)
	}
	f.mu.Lock()
	f.cache = cache
	f.forgetUnmountedStatfs(partitions)
	f.mu.Unlock()
	slices.SortFunc(items, func(a, b monitoringapi.FilesystemInfo) int {
		return cmp.Or(strings.Compare(a.Mountpoint, b.Mountpoint), strings.Compare(a.Device, b.Device))
	})
	return items
}

// errStatfsPending is a mount whose statfs has not returned yet.
var errStatfsPending = errors.New("statfs still running")

// usage returns the mount's usage from a statfs this collection starts and
// waits up to statfsWait for, or from one an earlier collection started that
// has returned since. A call still running is not waited for again.
func (f *Filesystems) usage(ctx context.Context, mountpoint string) (*psutilDisk.UsageStat, error) {
	f.mu.Lock()
	call := f.statfss[mountpoint]
	started := false
	if call != nil {
		select {
		case <-call.done:
			delete(f.statfss, mountpoint)
			if time.Since(call.at) > lateStatfsMaxAge {
				call = nil
			}
		default:
			f.mu.Unlock()
			return nil, errStatfsPending
		}
	}
	if call == nil {
		call = &statfsCall{done: make(chan struct{})}
		if f.statfss == nil {
			f.statfss = make(map[string]*statfsCall)
		}
		f.statfss[mountpoint] = call
		started = true
		// The call outlives this request when the mount does not answer, so
		// it does not take the request's cancellation; statfs ignores it.
		go func(ctx context.Context) {
			call.usage, call.err = liveDiskUsage(ctx, mountpoint)
			call.at = time.Now()
			close(call.done)
		}(context.WithoutCancel(ctx))
	}
	f.mu.Unlock()
	if !started {
		return call.usage, call.err
	}

	timer := time.NewTimer(statfsWait)
	defer timer.Stop()
	select {
	case <-call.done:
	case <-timer.C:
		return nil, errStatfsPending
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	f.mu.Lock()
	if f.statfss[mountpoint] == call {
		delete(f.statfss, mountpoint)
	}
	f.mu.Unlock()
	return call.usage, call.err
}

// forgetUnmountedStatfs drops the finished calls of mounts that are gone; a
// call still running stays until it returns and a later listing drops it.
// The caller holds f.mu.
func (f *Filesystems) forgetUnmountedStatfs(partitions []psutilDisk.PartitionStat) {
	for mountpoint, call := range f.statfss {
		if slices.ContainsFunc(partitions, func(p psutilDisk.PartitionStat) bool { return p.Mountpoint == mountpoint }) {
			continue
		}
		select {
		case <-call.done:
			delete(f.statfss, mountpoint)
		default:
		}
	}
}

// freshTrackedFilesystems copies the collector's recent usage figures out from
// under the app mutex, keyed by mountpoint.
func freshTrackedFilesystems(fsStats map[string]*systemmodel.FsStats, partitions []psutilDisk.PartitionStat, now time.Time) map[string]cachedFilesystem {
	fresh := make(map[string]cachedFilesystem)
	for _, partition := range partitions {
		tracked := trackedFilesystemStats(fsStats, partition.Mountpoint)
		if tracked == nil || tracked.Device != partition.Device || tracked.FSType != partition.Fstype || tracked.UsageAt.IsZero() || now.Sub(tracked.UsageAt) >= ReuseWindow {
			continue
		}
		fresh[partition.Mountpoint] = cachedFilesystem{info: filesystemInfoFromTracked(tracked, partition), at: tracked.UsageAt}
	}
	return fresh
}

func reusableCachedFilesystem(cached cachedFilesystem, found bool, partition psutilDisk.PartitionStat, root bool, duration time.Duration, now time.Time) (monitoringapi.FilesystemInfo, bool) {
	if !found || root || duration <= 0 || cached.info.Device != partition.Device || cached.info.FSType != partition.Fstype || cached.at.IsZero() || now.Before(cached.at) || now.Sub(cached.at) >= duration {
		return monitoringapi.FilesystemInfo{}, false
	}
	info := cached.info
	readOnly := disk.HasReadOnlyOpt(partition.Opts)
	info.ReadOnly = &readOnly
	return info, true
}

func trackedFilesystemStats(stats map[string]*systemmodel.FsStats, mountpoint string) *systemmodel.FsStats {
	for _, value := range stats {
		if value != nil && value.Mountpoint == mountpoint {
			return value
		}
	}
	return nil
}

func filesystemInfoFromTracked(stats *systemmodel.FsStats, partition psutilDisk.PartitionStat) monitoringapi.FilesystemInfo {
	readOnly := disk.HasReadOnlyOpt(partition.Opts)
	return monitoringapi.FilesystemInfo{Device: partition.Device, Mountpoint: partition.Mountpoint, FSType: partition.Fstype, ReadOnly: &readOnly, Total: stats.TotalBytes, Used: stats.UsedBytes, Free: stats.FreeBytes, UsedPercent: stats.UsedPercent, InodesTotal: optional(stats.InodesTotal), InodesUsed: optional(stats.InodesUsed), InodesFree: optional(stats.InodesFree), InodesUsedPercent: optional(stats.InodesUsedPercent)}
}

func filesystemInfoFromUsage(partition psutilDisk.PartitionStat, usage *psutilDisk.UsageStat) monitoringapi.FilesystemInfo {
	readOnly := disk.HasReadOnlyOpt(partition.Opts)
	return monitoringapi.FilesystemInfo{
		Device: partition.Device, Mountpoint: partition.Mountpoint, FSType: partition.Fstype, ReadOnly: &readOnly,
		Total: usage.Total, Used: usage.Used, Free: usage.Free, UsedPercent: usage.UsedPercent,
		InodesTotal: optional(usage.InodesTotal), InodesUsed: optional(usage.InodesUsed),
		InodesFree: optional(usage.InodesFree), InodesUsedPercent: optional(usage.InodesUsedPercent),
	}
}

func isPseudoFS(partition psutilDisk.PartitionStat) bool {
	if strings.HasPrefix(partition.Device, "/dev/") {
		return false
	}
	switch partition.Fstype {
	case "proc", "sysfs", "devtmpfs", "devpts", "tmpfs", "cgroup", "cgroup2", "pstore",
		"securityfs", "debugfs", "tracefs", "configfs", "overlay", "squashfs", "ramfs",
		"bpf", "nsfs", "autofs", "fusectl", "binfmt_misc", "efivarfs", "hugetlbfs",
		"mqueue", "rpc_pipefs":
		return true
	case "ext2", "ext3", "ext4", "xfs", "btrfs", "zfs", "f2fs", "reiserfs", "jfs", "ntfs", "vfat", "exfat":
		return false
	default:
		// Network and FUSE mounts are real filesystems even when their source
		// is not under /dev.
		return false
	}
}

// optional omits zero inode counters from the wire payload.
func optional[T comparable](value T) *T {
	var zero T
	if value == zero {
		return nil
	}
	return &value
}
