package system

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	gopsdisk "github.com/shirou/gopsutil/v4/disk"
)

var (
	diskRateStateLock sync.Mutex
	lastDiskCounters  = map[string]gopsdisk.IOCountersStat{}
	lastDiskSampleAt  time.Time

	diskCounterSampler  = sampleDiskCounters
	sysBlockExists      = defaultSysBlockExists
	sysBlockDeviceExist = defaultSysBlockDeviceExists
	diskClock           = time.Now
)

func sampleDiskCounters(ctx context.Context) map[string]gopsdisk.IOCountersStat {
	stats, err := gopsdisk.IOCountersWithContext(ctx)
	if err != nil {
		return map[string]gopsdisk.IOCountersStat{}
	}

	result := make(map[string]gopsdisk.IOCountersStat, len(stats))
	for name, stat := range stats {
		if err := ctx.Err(); err != nil {
			return result
		}
		if !isPhysicalDiskCounter(ctx, name) {
			continue
		}
		result[name] = stat
	}

	return result
}

func FetchDiskThroughput(ctx context.Context) (apischema.DiskThroughputResponse, error) {
	if err := ctx.Err(); err != nil {
		return apischema.DiskThroughputResponse{}, err
	}
	diskRateStateLock.Lock()
	current := diskCounterSampler(ctx)
	currentAt := diskClock()
	previous := lastDiskCounters
	previousAt := lastDiskSampleAt
	lastDiskCounters = current
	lastDiskSampleAt = currentAt
	diskRateStateLock.Unlock()

	intervalSeconds := 0.0
	if !previousAt.IsZero() {
		intervalSeconds = currentAt.Sub(previousAt).Seconds()
	}

	return buildDiskThroughputResponse(previous, current, intervalSeconds), nil
}

func buildDiskThroughputResponse(previous, current map[string]gopsdisk.IOCountersStat, intervalSeconds float64) apischema.DiskThroughputResponse {
	response := apischema.DiskThroughputResponse{
		IntervalSeconds: intervalSeconds,
		Devices:         make([]apischema.DiskThroughputDevice, 0, len(current)),
	}

	for name, currentStat := range current {
		previousStat, ok := previous[name]
		if !ok {
			previousStat = currentStat
		}

		device := apischema.DiskThroughputDevice{
			Name:             name,
			ReadBytesPerSec:  counterRate(previousStat.ReadBytes, currentStat.ReadBytes, intervalSeconds),
			WriteBytesPerSec: counterRate(previousStat.WriteBytes, currentStat.WriteBytes, intervalSeconds),
			ReadOpsPerSec:    counterRate(previousStat.ReadCount, currentStat.ReadCount, intervalSeconds),
			WriteOpsPerSec:   counterRate(previousStat.WriteCount, currentStat.WriteCount, intervalSeconds),
		}

		response.ReadBytesPerSec += device.ReadBytesPerSec
		response.WriteBytesPerSec += device.WriteBytesPerSec
		response.ReadOpsPerSec += device.ReadOpsPerSec
		response.WriteOpsPerSec += device.WriteOpsPerSec
		response.Devices = append(response.Devices, device)
	}

	sort.Slice(response.Devices, func(i, j int) bool {
		return response.Devices[i].Name < response.Devices[j].Name
	})

	return response
}

func counterRate(previous, current uint64, intervalSeconds float64) float64 {
	if intervalSeconds <= 0 || current < previous {
		return 0
	}
	return float64(current-previous) / intervalSeconds
}

func isPhysicalDiskCounter(ctx context.Context, name string) bool {
	if name == "" || strings.Contains(name, "/") {
		return false
	}

	switch {
	case strings.HasPrefix(name, "loop"),
		strings.HasPrefix(name, "ram"),
		strings.HasPrefix(name, "zram"),
		strings.HasPrefix(name, "dm-"),
		strings.HasPrefix(name, "md"),
		strings.HasPrefix(name, "sr"),
		strings.HasPrefix(name, "fd"):
		return false
	}

	if err := ctx.Err(); err != nil {
		return false
	}
	if !sysBlockExists(ctx, name) {
		return false
	}

	if err := ctx.Err(); err != nil {
		return false
	}
	return sysBlockDeviceExist(ctx, name)
}

func defaultSysBlockExists(ctx context.Context, name string) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join("/sys/block", name))
	return err == nil
}

func defaultSysBlockDeviceExists(ctx context.Context, name string) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join("/sys/block", name, "device"))
	return err == nil
}
