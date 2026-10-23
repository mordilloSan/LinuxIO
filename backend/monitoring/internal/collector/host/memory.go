package host

import (
	"context"
	"log/slog"

	psutilHost "github.com/shirou/gopsutil/v4/host"
	psutilLoad "github.com/shirou/gopsutil/v4/load"
	psutilMem "github.com/shirou/gopsutil/v4/mem"

	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/units"
)

// UpdateCore fills the battery, CPU, load and memory sections of the stats.
func (m *Manager) UpdateCore(ctx context.Context, cacheTimeMs uint16, systemStats *systemmodel.Stats) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	updateBatteryStats(systemStats)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := updateCPUMetrics(ctx, cacheTimeMs, systemStats); err != nil {
		return err
	}
	if err := updateLoadStats(ctx, systemStats); err != nil {
		return err
	}
	return m.updateMemoryStats(ctx, systemStats)
}

func updateBatteryStats(systemStats *systemmodel.Stats) {
	if batteryPercent, batteryState, err := GetBatteryStats(); err == nil {
		systemStats.Battery[0] = batteryPercent
		systemStats.Battery[1] = batteryState
	}
}

func updateCPUMetrics(ctx context.Context, cacheTimeMs uint16, systemStats *systemmodel.Stats) error {
	cpuMetrics, err := getCpuMetrics(ctx, cacheTimeMs)
	if err == nil {
		systemStats.Cpu = units.TwoDecimals(cpuMetrics.Total)
		systemStats.CpuBreakdown = []float64{
			units.TwoDecimals(cpuMetrics.User),
			units.TwoDecimals(cpuMetrics.System),
			units.TwoDecimals(cpuMetrics.Iowait),
			units.TwoDecimals(cpuMetrics.Steal),
			units.TwoDecimals(cpuMetrics.Idle),
		}
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	} else {
		slog.Error("Error getting cpu metrics", "err", err)
	}

	if perCoreUsage, err := getPerCoreCpuUsage(ctx, cacheTimeMs); err == nil {
		systemStats.CpuCoresUsage = perCoreUsage
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return nil
}

func updateLoadStats(ctx context.Context, systemStats *systemmodel.Stats) error {
	avgstat, err := psutilLoad.AvgWithContext(ctx)
	if err == nil {
		systemStats.LoadAvg[0] = avgstat.Load1
		systemStats.LoadAvg[1] = avgstat.Load5
		systemStats.LoadAvg[2] = avgstat.Load15
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	slog.Error("Error getting load average", "err", err)
	return nil
}

func (m *Manager) updateMemoryStats(ctx context.Context, systemStats *systemmodel.Stats) error {
	v, err := psutilMem.VirtualMemoryWithContext(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return nil
	}

	systemStats.MemoryBytes = systemmodel.MemoryBytes{
		Total: v.Total, Available: v.Available, Free: v.Free, Cached: v.Cached,
		Buffers: v.Buffers, Shared: v.Shared, SwapTotal: v.SwapTotal, SwapFree: v.SwapFree,
	}

	swapUsed := usedSwapBytes(v.SwapTotal, v.SwapFree, v.SwapCached)
	systemStats.Swap = units.BytesToGigabytes(v.SwapTotal)
	systemStats.SwapUsed = units.BytesToGigabytes(swapUsed)
	if v.SwapTotal > 0 {
		systemStats.SwapPct = units.TwoDecimals(float64(swapUsed) / float64(v.SwapTotal) * 100)
	}
	systemStats.MemAvailable = units.BytesToGigabytes(v.Available)
	systemStats.MemCached = units.BytesToGigabytes(v.Cached)
	systemStats.MemBuffers = units.BytesToGigabytes(v.Buffers)
	cacheBuff := v.Cached + v.Buffers - v.Shared
	if cacheBuff <= 0 {
		cacheBuff = max(v.Total-v.Free-v.Used, 0)
	}
	if m.memCalc == "htop" {
		v.Used = v.Total - (v.Free + cacheBuff)
		v.UsedPercent = float64(v.Used) / float64(v.Total) * 100.0
	}
	var arcBytes uint64
	if m.zfs {
		arcBytes, _ = ARCSize()
		if arcBytes > 0 && arcBytes < v.Used {
			v.Used -= arcBytes
			v.UsedPercent = float64(v.Used) / float64(v.Total) * 100.0
			systemStats.MemZfsArc = units.BytesToGigabytes(arcBytes)
		}
	}
	systemStats.Mem = units.BytesToGigabytes(v.Total)
	systemStats.MemBuffCache = units.BytesToGigabytes(cacheBuff)
	systemStats.MemUsed = units.BytesToGigabytes(v.Used)
	systemStats.MemPct = units.TwoDecimals(v.UsedPercent)
	systemStats.MemoryBytes.Used = v.Used
	systemStats.MemoryBytes.ZFSArc = arcBytes
	return nil
}

func usedSwapBytes(total, free, cached uint64) uint64 {
	if free >= total {
		return 0
	}
	used := total - free
	if cached >= used {
		return 0
	}
	return used - cached
}

// UpdateInfo copies the headline figures of the stats into Info.
func (m *Manager) UpdateInfo(ctx context.Context, systemStats *systemmodel.Stats) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info := &m.Info
	info.Cpu = systemStats.Cpu
	info.LoadAvg = systemStats.LoadAvg
	info.MemPct = systemStats.MemPct
	info.DiskPct = systemStats.DiskPct
	info.Battery = systemStats.Battery
	if uptime, err := psutilHost.UptimeWithContext(ctx); err == nil {
		info.Uptime = uptime
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	info.BandwidthBytes = systemStats.Bandwidth[0] + systemStats.Bandwidth[1]
	info.Threads = m.Details.Threads
	return nil
}
