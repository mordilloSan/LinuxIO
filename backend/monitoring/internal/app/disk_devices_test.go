package app

import (
	"context"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/disk"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

func TestBlockDeviceRatesFilterAndDelta(t *testing.T) {
	m := newFsManager()
	physical := func(string) bool { return true }
	now := time.Now()
	first := map[string]disk.IOCountersStat{
		"sda":   {ReadBytes: 1000, WriteBytes: 0, ReadCount: 10, WriteCount: 0},
		"loop0": {ReadBytes: 5},
	}
	var stats system.Stats
	m.applyBlockDeviceCounters(1010, &stats, first, now, func(name string) bool { return name != "loop0" && physical(name) })
	if len(stats.DiskDevices) != 0 {
		t.Fatalf("first sample must carry no rates, got %v", stats.DiskDevices)
	}
	second := map[string]disk.IOCountersStat{
		"sda":   {ReadBytes: 3000, WriteBytes: 500, ReadCount: 30, WriteCount: 5},
		"loop0": {ReadBytes: 50},
	}
	stats = system.Stats{}
	m.applyBlockDeviceCounters(1010, &stats, second, now.Add(2*time.Second), func(name string) bool { return name != "loop0" })
	got := stats.DiskDevices["sda"]
	if got.ReadBytesPerSec != 1000 || got.WriteBytesPerSec != 250 || got.ReadOpsPerSec != 10 || got.WriteOpsPerSec != 2.5 {
		t.Fatalf("rates = %+v", got)
	}
	if _, ok := stats.DiskDevices["loop0"]; ok {
		t.Fatal("loop device must be filtered")
	}

	// A counter reset (device re-enumerated, driver reload) must skip the
	// sample rather than underflow the uint64 subtraction.
	third := map[string]disk.IOCountersStat{
		"sda": {ReadBytes: 4000, WriteBytes: 600, ReadCount: 2, WriteCount: 6},
	}
	stats = system.Stats{}
	m.applyBlockDeviceCounters(1010, &stats, third, now.Add(4*time.Second), func(string) bool { return true })
	if got, ok := stats.DiskDevices["sda"]; ok {
		t.Fatalf("read-count reset must be skipped, got %+v", got)
	}
	fourth := map[string]disk.IOCountersStat{
		"sda": {ReadBytes: 5000, WriteBytes: 700, ReadCount: 10, WriteCount: 1},
	}
	stats = system.Stats{}
	m.applyBlockDeviceCounters(1010, &stats, fourth, now.Add(6*time.Second), func(string) bool { return true })
	if got, ok := stats.DiskDevices["sda"]; ok {
		t.Fatalf("write-count reset must be skipped, got %+v", got)
	}
}

func TestBlockDeviceReseedsStaleLiveKey(t *testing.T) {
	m := newFsManager()
	now := time.Now()
	m.devicePrev = map[uint16]map[string]prevDisk{
		collectorDataKeyMs: {"sda": {readBytes: 1000, readCount: 10, at: now.Add(-2 * time.Second)}},
		1010:               {"sda": {readBytes: 1, readCount: 1, at: now.Add(-time.Hour)}},
	}
	var stats system.Stats
	m.applyBlockDeviceCounters(1010, &stats, map[string]disk.IOCountersStat{
		"sda": {ReadBytes: 3000, ReadCount: 30},
	}, now, func(string) bool { return true })

	// The collector's baseline replaced the hour-old one, so the rate covers
	// the two-second collector window, not the hour.
	got := stats.DiskDevices["sda"]
	if got.ReadBytesPerSec != 1000 || got.ReadOpsPerSec != 10 {
		t.Fatalf("device baseline not reseeded from collector: %+v", got)
	}
}

func TestBlockDeviceReseedKeepsFreshLiveKey(t *testing.T) {
	m := newFsManager()
	now := time.Now()
	m.devicePrev = map[uint16]map[string]prevDisk{
		collectorDataKeyMs: {"sda": {readBytes: 1000, readCount: 10, at: now.Add(-2 * time.Second)}},
		1010:               {"sda": {readBytes: 500, readCount: 5, at: now.Add(-time.Second)}},
	}
	var stats system.Stats
	m.applyBlockDeviceCounters(1010, &stats, map[string]disk.IOCountersStat{
		"sda": {ReadBytes: 1500, ReadCount: 15},
	}, now, func(string) bool { return true })

	got := stats.DiskDevices["sda"]
	if got.ReadBytesPerSec != 1000 || got.ReadOpsPerSec != 10 {
		t.Fatalf("fresh live baseline must be kept: %+v", got)
	}
}

func TestIsPhysicalBlockDeviceRejectsVirtualNames(t *testing.T) {
	for _, name := range []string{"loop0", "ram1", "zram0", "dm-0", "md0", "sr0", "fd0", "", "a/b"} {
		if isPhysicalBlockDevice(context.Background(), name, func(string) bool { return true }) {
			t.Fatalf("%q must be rejected", name)
		}
	}
	if !isPhysicalBlockDevice(context.Background(), "nvme0n1", func(string) bool { return true }) {
		t.Fatal("nvme0n1 must pass when sysfs says it has a device")
	}
}
