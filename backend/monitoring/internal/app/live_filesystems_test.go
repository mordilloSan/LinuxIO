package app

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/disk"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

func TestIsPseudoFSKeepsRealNetworkAndBlockMounts(t *testing.T) {
	tests := []struct {
		partition disk.PartitionStat
		pseudo    bool
	}{
		{partition: disk.PartitionStat{Device: "proc", Fstype: "proc"}, pseudo: true},
		{partition: disk.PartitionStat{Device: "overlay", Fstype: "overlay"}, pseudo: true},
		{partition: disk.PartitionStat{Device: "/dev/nvme0n1p1", Fstype: "ext4"}},
		{partition: disk.PartitionStat{Device: "server:/export", Fstype: "nfs4"}},
	}
	for _, test := range tests {
		if got := isPseudoFS(test.partition); got != test.pseudo {
			t.Errorf("isPseudoFS(%+v) = %v, want %v", test.partition, got, test.pseudo)
		}
	}
}

func TestFilesystemInfoCacheTracksMountIdentityAndDiscovery(t *testing.T) {
	partitions := []disk.PartitionStat{
		{Device: "/dev/root", Mountpoint: "/", Fstype: "ext4"},
		{Device: "/dev/data", Mountpoint: "/data", Fstype: "ext4"},
		{Device: "/dev/data", Mountpoint: "/other", Fstype: "xfs"},
	}
	usage := map[string]disk.UsageStat{
		"/":      {Total: 100, Used: 10, Free: 90},
		"/data":  {Total: 200, Used: 20, Free: 180},
		"/other": {Total: 300, Used: 30, Free: 270},
		"/new":   {Total: 400, Used: 40, Free: 360},
	}
	var usageCalls []string
	oldPartitions, oldUsage := livePartitions, liveDiskUsage
	livePartitions = func(context.Context, bool) ([]disk.PartitionStat, error) {
		return append([]disk.PartitionStat(nil), partitions...), nil
	}
	liveDiskUsage = func(_ context.Context, mountpoint string) (*disk.UsageStat, error) {
		usageCalls = append(usageCalls, mountpoint)
		item := usage[mountpoint]
		return &item, nil
	}
	t.Cleanup(func() {
		livePartitions, liveDiskUsage = oldPartitions, oldUsage
	})

	a := &App{fsManager: &fsManager{
		fsStats:                map[string]*system.FsStats{},
		liveFilesystemCache:    map[string]cachedLiveFilesystem{},
		diskUsageCacheDuration: time.Hour,
	}}
	first := a.filesystemInfo(context.Background())
	if len(first) != 3 || len(usageCalls) != 3 {
		t.Fatalf("first filesystem sample = %#v, usage calls = %v", first, usageCalls)
	}
	second := a.filesystemInfo(context.Background())
	if len(second) != 3 || !reflect.DeepEqual(first, second) || len(usageCalls) != 4 {
		t.Fatalf("cached filesystem sample = %#v, usage calls = %v", second, usageCalls)
	}

	partitions[1] = disk.PartitionStat{Device: "/dev/new-data", Mountpoint: "/data", Fstype: "ext4"}
	partitions = append(partitions[:2], disk.PartitionStat{Device: "/dev/new", Mountpoint: "/new", Fstype: "ext4"})
	third := a.filesystemInfo(context.Background())
	if len(third) != 3 || third[0].Mountpoint != "/" || third[1].Mountpoint != "/data" || third[2].Mountpoint != "/new" {
		t.Fatalf("rediscovered filesystems = %#v", third)
	}
	if len(usageCalls) != 7 {
		t.Fatalf("source replacement/new mount should refresh usage, calls = %v", usageCalls)
	}
	if _, ok := a.fsManager.liveFilesystemCache["/other"]; ok {
		t.Fatal("vanished mount remained in cache")
	}
}
