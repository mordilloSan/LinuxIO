package live

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	psutilDisk "github.com/shirou/gopsutil/v4/disk"

	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

type fakeTracker struct {
	stats      map[string]*systemmodel.FsStats
	usageCache time.Duration
}

func (f fakeTracker) Tracked() map[string]*systemmodel.FsStats { return f.stats }
func (f fakeTracker) UsageCacheDuration() time.Duration        { return f.usageCache }

func TestIsPseudoFSKeepsRealNetworkAndBlockMounts(t *testing.T) {
	tests := []struct {
		partition psutilDisk.PartitionStat
		pseudo    bool
	}{
		{partition: psutilDisk.PartitionStat{Device: "proc", Fstype: "proc"}, pseudo: true},
		{partition: psutilDisk.PartitionStat{Device: "overlay", Fstype: "overlay"}, pseudo: true},
		{partition: psutilDisk.PartitionStat{Device: "/dev/nvme0n1p1", Fstype: "ext4"}},
		{partition: psutilDisk.PartitionStat{Device: "server:/export", Fstype: "nfs4"}},
	}
	for _, test := range tests {
		if got := isPseudoFS(test.partition); got != test.pseudo {
			t.Errorf("isPseudoFS(%+v) = %v, want %v", test.partition, got, test.pseudo)
		}
	}
}

func TestFilesystemInfoCacheTracksMountIdentityAndDiscovery(t *testing.T) {
	partitions := []psutilDisk.PartitionStat{
		{Device: "/dev/root", Mountpoint: "/", Fstype: "ext4"},
		{Device: "/dev/data", Mountpoint: "/data", Fstype: "ext4"},
		{Device: "/dev/data", Mountpoint: "/other", Fstype: "xfs"},
	}
	usage := map[string]psutilDisk.UsageStat{
		"/":      {Total: 100, Used: 10, Free: 90},
		"/data":  {Total: 200, Used: 20, Free: 180},
		"/other": {Total: 300, Used: 30, Free: 270},
		"/new":   {Total: 400, Used: 40, Free: 360},
	}
	var usageCalls []string
	oldPartitions, oldUsage := livePartitions, liveDiskUsage
	livePartitions = func(context.Context, bool) ([]psutilDisk.PartitionStat, error) {
		return append([]psutilDisk.PartitionStat(nil), partitions...), nil
	}
	liveDiskUsage = func(_ context.Context, mountpoint string) (*psutilDisk.UsageStat, error) {
		usageCalls = append(usageCalls, mountpoint)
		item := usage[mountpoint]
		return &item, nil
	}
	t.Cleanup(func() {
		livePartitions, liveDiskUsage = oldPartitions, oldUsage
	})

	var mu sync.Mutex
	filesystems := &Filesystems{}
	tracker := fakeTracker{stats: map[string]*systemmodel.FsStats{}, usageCache: time.Hour}
	first := filesystems.Info(context.Background(), &mu, tracker)
	if len(first) != 3 || len(usageCalls) != 3 {
		t.Fatalf("first filesystem sample = %#v, usage calls = %v", first, usageCalls)
	}
	second := filesystems.Info(context.Background(), &mu, tracker)
	if len(second) != 3 || !reflect.DeepEqual(first, second) || len(usageCalls) != 4 {
		t.Fatalf("cached filesystem sample = %#v, usage calls = %v", second, usageCalls)
	}

	partitions[1] = psutilDisk.PartitionStat{Device: "/dev/new-data", Mountpoint: "/data", Fstype: "ext4"}
	partitions = append(partitions[:2], psutilDisk.PartitionStat{Device: "/dev/new", Mountpoint: "/new", Fstype: "ext4"})
	third := filesystems.Info(context.Background(), &mu, tracker)
	if len(third) != 3 || third[0].Mountpoint != "/" || third[1].Mountpoint != "/data" || third[2].Mountpoint != "/new" {
		t.Fatalf("rediscovered filesystems = %#v", third)
	}
	if len(usageCalls) != 7 {
		t.Fatalf("source replacement/new mount should refresh usage, calls = %v", usageCalls)
	}
	if _, ok := filesystems.cache["/other"]; ok {
		t.Fatal("vanished mount remained in cache")
	}
}
