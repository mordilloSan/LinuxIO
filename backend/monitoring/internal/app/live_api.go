package app

import (
	"context"
	"time"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

const bytesPerMiB = 1024 * 1024

// Live returns the LinuxIO-shaped live payload. Same request semantics as any
// live key: one-second reuse via liveCurrentData; a request during a collector
// pass waits on the app mutex.
func (a *App) Live(ctx context.Context) (monitoringapi.Live, error) {
	if err := ctx.Err(); err != nil {
		return monitoringapi.Live{}, err
	}
	data, capturedAt, err := a.liveCurrentData(ctx, liveSampleKey(liveLiveEndpoint), true, true)
	if err != nil {
		return monitoringapi.Live{}, err
	}
	threads := 0
	if data.Details != nil {
		threads = data.Details.Threads
	}
	telemetry, telemetryAt := a.lastTelemetry()
	return buildLive(data, capturedAt, threads, telemetry, telemetryAt, 3*a.CollectorInterval()), nil
}

func (a *App) lastTelemetry() ([]container.Telemetry, time.Time) {
	a.telemetryMu.RLock()
	defer a.telemetryMu.RUnlock()
	return a.telemetry, a.telemetryAt
}

func (a *App) rememberTelemetry(items []container.Telemetry, at time.Time) {
	a.telemetryMu.Lock()
	a.telemetry = items
	a.telemetryAt = at
	a.telemetryMu.Unlock()
}

func buildLive(data *system.CombinedData, capturedAt time.Time, threads int, telemetry []container.Telemetry, telemetryAt time.Time, freshFor time.Duration) monitoringapi.Live {
	stats := data.Stats
	live := monitoringapi.Live{
		CapturedAtMs:  capturedAt.UTC().UnixMilli(),
		UptimeSeconds: data.Info.Uptime,
		CPU: monitoringapi.LiveCPU{
			Percent:        stats.Cpu,
			PerCorePercent: make([]float64, 0, len(stats.CpuCoresUsage)),
			LoadAverage:    stats.LoadAvg,
		},
		Memory: monitoringapi.LiveMemory{
			TotalBytes:     stats.MemoryBytes.Total,
			UsedBytes:      stats.MemoryBytes.Used,
			AvailableBytes: stats.MemoryBytes.Available,
			FreeBytes:      stats.MemoryBytes.Free,
			CachedBytes:    stats.MemoryBytes.Cached,
			BuffersBytes:   stats.MemoryBytes.Buffers,
			SharedBytes:    stats.MemoryBytes.Shared,
			SwapTotalBytes: stats.MemoryBytes.SwapTotal,
			SwapFreeBytes:  stats.MemoryBytes.SwapFree,
			ZFSArcBytes:    stats.MemoryBytes.ZFSArc,
		},
		Disks:      map[string]monitoringapi.LiveDiskRates{},
		Interfaces: map[string]monitoringapi.LiveInterface{},
		Containers: monitoringapi.LiveContainers{CapturedAtMs: capturedAt.UTC().UnixMilli(), Items: []monitoringapi.LiveContainer{}},
	}
	for _, core := range stats.CpuCoresUsage {
		live.CPU.PerCorePercent = append(live.CPU.PerCorePercent, float64(core))
	}
	if len(stats.CpuBreakdown) == 5 {
		live.CPU.Breakdown = monitoringapi.LiveCPUBreakdown{
			User: stats.CpuBreakdown[0], System: stats.CpuBreakdown[1], IOWait: stats.CpuBreakdown[2],
			Steal: stats.CpuBreakdown[3], Idle: stats.CpuBreakdown[4],
		}
	}
	for name, rates := range stats.DiskDevices {
		live.Disks[name] = monitoringapi.LiveDiskRates(rates)
		live.DiskIO.ReadBytesPerSec += rates.ReadBytesPerSec
		live.DiskIO.WriteBytesPerSec += rates.WriteBytesPerSec
		live.DiskIO.ReadOpsPerSec += rates.ReadOpsPerSec
		live.DiskIO.WriteOpsPerSec += rates.WriteOpsPerSec
	}
	for name, values := range stats.NetworkInterfaces {
		live.Interfaces[name] = monitoringapi.LiveInterface{
			TxBytesPerSec: float64(values[0]), RxBytesPerSec: float64(values[1]),
			TxBytesTotal: values[2], RxBytesTotal: values[3],
		}
	}

	telemetryFresh := !telemetryAt.IsZero() && capturedAt.Sub(telemetryAt) <= freshFor
	blockRates := map[string]container.Telemetry{}
	if telemetryFresh {
		for _, item := range telemetry {
			blockRates[item.ID] = item
		}
	}
	cpuMultiplier := float64(max(threads, 1))
	for _, ctr := range data.Containers {
		if ctr == nil {
			continue
		}
		memBytes := uint64(ctr.Mem * bytesPerMiB)
		live.Memory.DockerUsedBytes += memBytes
		item := monitoringapi.LiveContainer{
			ID: ctr.Id, Name: ctr.Name,
			CPUPercent:    ctr.Cpu * cpuMultiplier,
			MemoryBytes:   memBytes,
			TxBytesPerSec: float64(ctr.Bandwidth[0]), RxBytesPerSec: float64(ctr.Bandwidth[1]),
		}
		if rec, ok := blockRates[ctr.Id]; ok {
			read, write := float64(rec.DiskReadBytesPerSecond), float64(rec.DiskWriteBytesPerSecond)
			item.BlockReadBytesPerSec, item.BlockWriteBytesPerSec = &read, &write
		}
		live.Containers.Items = append(live.Containers.Items, item)
	}
	return live
}
