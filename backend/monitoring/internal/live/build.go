// Package live assembles the LinuxIO live payload (monitoringapi.Live). It owns
// the wire DTOs of the live API, so the collectors keep their own models and
// never import them: the sections that are read straight from the host rather
// than from the sampled data (filesystems, sensors, GPUs, SMART power) are
// built here too.
package live

import (
	"maps"
	"slices"
	"time"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	containermodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

// ReuseWindow bounds how often a live key collects. Requests inside the window
// share the newest sample; captured_at reports that sample's time.
const ReuseWindow = time.Second

const bytesPerMiB = 1024 * 1024

// Extras are the optional live sections collected next to the host sample.
type Extras struct {
	Filesystems []monitoringapi.FilesystemInfo
	Sensors     []monitoringapi.SensorGroup
	Frequencies []float64
	GPUs        map[string]monitoringapi.LiveGPU
	Smart       map[string]monitoringapi.LiveSmart
}

// Apply copies the extras into l, so callers cannot mutate what other requests
// share, and substitutes empty values for absent sections.
func (e Extras) Apply(l *monitoringapi.Live) {
	l.Filesystems = clonedOrEmpty(e.Filesystems)
	l.Sensors = clonedOrEmpty(e.Sensors)
	l.CPU.FrequenciesMHz = clonedOrEmpty(e.Frequencies)
	l.GPUs = clonedMapOrEmpty(e.GPUs)
	l.Smart = clonedMapOrEmpty(e.Smart)
	l.CPU.Temperatures = CPUTemperatures(l.Sensors)
}

func clonedOrEmpty[E any](items []E) []E {
	if items == nil {
		return []E{}
	}
	return slices.Clone(items)
}

func clonedMapOrEmpty[K comparable, V any](items map[K]V) map[K]V {
	if items == nil {
		return map[K]V{}
	}
	return maps.Clone(items)
}

// Build maps one sampled host snapshot, the last container telemetry and its
// capture time onto the live payload. Telemetry older than freshFor at
// capturedAt is ignored. Extras fills the sections that are not part of the
// sample.
func Build(data *systemmodel.CombinedData, capturedAt time.Time, threads int, telemetry []containermodel.Telemetry, telemetryAt time.Time, freshFor time.Duration) monitoringapi.Live {
	stats := data.Stats
	live := monitoringapi.Live{
		CapturedAtMs:  capturedAt.UTC().UnixMilli(),
		UptimeSeconds: data.Info.Uptime,
		CPU: monitoringapi.LiveCPU{
			Percent:        stats.Cpu,
			PerCorePercent: make([]float64, 0, len(stats.CpuCoresUsage)),
			LoadAverage:    stats.LoadAvg,
			FrequenciesMHz: []float64{},
			Temperatures:   map[string]float64{},
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
		Disks:       map[string]monitoringapi.LiveDiskRates{},
		Interfaces:  map[string]monitoringapi.LiveInterface{},
		Filesystems: []monitoringapi.FilesystemInfo{},
		Sensors:     []monitoringapi.SensorGroup{},
		GPUs:        map[string]monitoringapi.LiveGPU{},
		Smart:       map[string]monitoringapi.LiveSmart{},
		Containers:  monitoringapi.LiveContainers{CapturedAtMs: capturedAt.UTC().UnixMilli(), Items: []monitoringapi.LiveContainer{}},
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
		if counters, ok := stats.NetworkInterfaceCounters[name]; ok {
			iface := live.Interfaces[name]
			iface.RxDropped = counters.RxDropped
			iface.RxErrors = counters.RxErrors
			iface.RxPackets = counters.RxPackets
			iface.TxDropped = counters.TxDropped
			iface.TxErrors = counters.TxErrors
			iface.TxPackets = counters.TxPackets
			live.Interfaces[name] = iface
		}
	}

	telemetryFresh := !telemetryAt.IsZero() && capturedAt.Sub(telemetryAt) <= freshFor
	blockRates := map[string]containermodel.Telemetry{}
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
