package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/disk"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

// Per-block-device rates for the LinuxIO live payload. Keyed by interval key
// like every other delta so live and collector samples never share a baseline.

func sysBlockHasDevice(name string) bool {
	_, err := os.Stat(filepath.Join("/sys/block", name, "device"))
	return err == nil
}

// isPhysicalBlockDevice mirrors the filter the bridge used for disk
// throughput: skip virtual and partition-like names, keep devices sysfs
// reports with a physical device node.
func isPhysicalBlockDevice(ctx context.Context, name string, hasDevice func(string) bool) bool {
	if ctx.Err() != nil || name == "" || strings.Contains(name, "/") {
		return false
	}
	for _, prefix := range []string{"loop", "ram", "zram", "dm-", "md", "sr", "fd"} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return hasDevice(name)
}

func (m *fsManager) updateBlockDeviceRates(ctx context.Context, cacheTimeMs uint16, systemStats *system.Stats) error {
	counters, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return nil
	}
	m.applyBlockDeviceCounters(cacheTimeMs, systemStats, counters, time.Now(), func(name string) bool {
		return isPhysicalBlockDevice(ctx, name, sysBlockHasDevice)
	})
	return nil
}

func (m *fsManager) applyBlockDeviceCounters(cacheTimeMs uint16, systemStats *system.Stats, counters map[string]disk.IOCountersStat, now time.Time, keep func(string) bool) {
	m.reseedFromCollector(cacheTimeMs, now)
	if m.devicePrev == nil {
		m.devicePrev = map[uint16]map[string]prevDisk{}
	}
	if m.devicePrev[cacheTimeMs] == nil {
		m.devicePrev[cacheTimeMs] = map[string]prevDisk{}
	}
	prevByName := m.devicePrev[cacheTimeMs]
	systemStats.DiskDevices = map[string]system.DiskDeviceRates{}
	for name, counter := range counters {
		if !keep(name) {
			continue
		}
		prev, hasPrev := prevByName[name]
		prevByName[name] = prevDiskFromCounter(counter, now)
		if !hasPrev {
			continue
		}
		seconds := now.Sub(prev.at).Seconds()
		if seconds <= 0 || counter.ReadBytes < prev.readBytes || counter.WriteBytes < prev.writeBytes ||
			counter.ReadCount < prev.readCount || counter.WriteCount < prev.writeCount {
			continue
		}
		systemStats.DiskDevices[name] = system.DiskDeviceRates{
			ReadBytesPerSec:  float64(counter.ReadBytes-prev.readBytes) / seconds,
			WriteBytesPerSec: float64(counter.WriteBytes-prev.writeBytes) / seconds,
			ReadOpsPerSec:    float64(counter.ReadCount-prev.readCount) / seconds,
			WriteOpsPerSec:   float64(counter.WriteCount-prev.writeCount) / seconds,
		}
	}
}
