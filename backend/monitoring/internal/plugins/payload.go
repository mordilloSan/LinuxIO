package plugins

import (
	containermodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

// The *Data types are the JSON payload of one plugin each; Snapshot fills them.
type CPUData struct {
	Cpu            float64                `json:"cpu_percent"`
	MaxCpu         float64                `json:"max_cpu_percent,omitzero"`
	CpuBreakdown   []float64              `json:"cpu_breakdown_percent,omitempty"`
	CpuCoresUsage  systemmodel.Uint8Slice `json:"cpu_cores_percent,omitempty"`
	CpuFrequencies []float64              `json:"cpu_frequencies_mhz,omitempty"`
}

type MemData struct {
	Mem          float64 `json:"memory_gb"`
	MaxMem       float64 `json:"max_memory_gb,omitzero"`
	MemUsed      float64 `json:"memory_used_gb"`
	MemPct       float64 `json:"memory_percent"`
	MemBuffCache float64 `json:"memory_buffer_cache_gb"`
	MemZfsArc    float64 `json:"memory_zfs_arc_gb,omitzero"`
	MemSwapTotal float64 `json:"memory_swap_total_gb"`
	MemSwapUsed  float64 `json:"memory_swap_used_gb"`
	MemSwapPct   float64 `json:"memory_swap_percent"`
	MemAvailable float64 `json:"memory_available_gb"`
	MemCached    float64 `json:"memory_cached_gb"`
	MemBuffers   float64 `json:"memory_buffers_gb"`
}

type SwapData struct {
	Swap     float64 `json:"swap_gb,omitzero"`
	SwapUsed float64 `json:"swap_used_gb,omitzero"`
}

type LoadData struct {
	LoadAvg [3]float64 `json:"load_average,omitempty"`
	Battery [2]uint16  `json:"battery,omitzero"`
}

type DiskIOData struct {
	DiskTotal      float64    `json:"disk_total_gb"`
	DiskUsed       float64    `json:"disk_used_gb"`
	DiskPct        float64    `json:"disk_percent"`
	DiskReadPs     float64    `json:"disk_read_mb_per_second,omitzero"`
	DiskWritePs    float64    `json:"disk_write_mb_per_second,omitzero"`
	MaxDiskReadPs  float64    `json:"max_disk_read_mb_per_second,omitzero"`
	MaxDiskWritePs float64    `json:"max_disk_write_mb_per_second,omitzero"`
	DiskIO         [2]uint64  `json:"disk_io_bytes_per_second,omitzero"`
	MaxDiskIO      [2]uint64  `json:"max_disk_io_bytes_per_second,omitzero"`
	DiskIoStats    [6]float64 `json:"disk_io_stats,omitzero"`
	MaxDiskIoStats [6]float64 `json:"max_disk_io_stats,omitzero"`
}

type FSData struct {
	ExtraFs map[string]*systemmodel.FsStats `json:"extra_filesystems,omitempty"`
}

type NetworkData struct {
	Bandwidth         [2]uint64            `json:"bandwidth_bytes_per_second,omitzero"`
	MaxBandwidth      [2]uint64            `json:"max_bandwidth_bytes_per_second,omitzero"`
	NetworkInterfaces map[string][4]uint64 `json:"network_interfaces,omitempty"`
}

type SensorsData struct {
	Temperatures map[string]float64 `json:"temperatures,omitempty"`
}

type containerSnapshotRecord struct {
	ID        string                      `json:"id"`
	Name      string                      `json:"name"`
	Image     string                      `json:"image,omitempty"`
	Ports     string                      `json:"ports,omitempty"`
	Status    string                      `json:"status,omitempty"`
	Health    containermodel.DockerHealth `json:"health,omitzero"`
	Cpu       float64                     `json:"cpu_percent"`
	Mem       float64                     `json:"memory_mb"`
	Bandwidth [2]uint64                   `json:"bandwidth_bytes,omitempty,omitzero"`
}

// Snapshot projects one collected sample onto the JSON payload of every plugin
// that is part of the persisted or live schema.
func Snapshot(data *systemmodel.CombinedData) map[string]any {
	stats := data.Stats
	return map[string]any{
		CPU: CPUData{
			Cpu:            stats.Cpu,
			MaxCpu:         stats.MaxCpu,
			CpuBreakdown:   stats.CpuBreakdown,
			CpuCoresUsage:  stats.CpuCoresUsage,
			CpuFrequencies: stats.CpuFrequencies,
		},
		Mem: MemData{
			Mem:          stats.Mem,
			MaxMem:       stats.MaxMem,
			MemUsed:      stats.MemUsed,
			MemPct:       stats.MemPct,
			MemBuffCache: stats.MemBuffCache,
			MemZfsArc:    stats.MemZfsArc,
			MemSwapTotal: stats.Swap,
			MemSwapUsed:  stats.SwapUsed,
			MemSwapPct:   stats.SwapPct,
			MemAvailable: stats.MemAvailable,
			MemCached:    stats.MemCached,
			MemBuffers:   stats.MemBuffers,
		},
		Swap: SwapData{
			Swap:     stats.Swap,
			SwapUsed: stats.SwapUsed,
		},
		Load: LoadData{
			LoadAvg: stats.LoadAvg,
			Battery: stats.Battery,
		},
		DiskIO: DiskIOData{
			DiskTotal:      stats.DiskTotal,
			DiskUsed:       stats.DiskUsed,
			DiskPct:        stats.DiskPct,
			DiskReadPs:     stats.DiskReadPs,
			DiskWritePs:    stats.DiskWritePs,
			MaxDiskReadPs:  stats.MaxDiskReadPs,
			MaxDiskWritePs: stats.MaxDiskWritePs,
			DiskIO:         stats.DiskIO,
			MaxDiskIO:      stats.MaxDiskIO,
			DiskIoStats:    stats.DiskIoStats,
			MaxDiskIoStats: stats.MaxDiskIoStats,
		},
		FS: FSData{
			ExtraFs: stats.ExtraFs,
		},
		Network: NetworkData{
			Bandwidth:         stats.Bandwidth,
			MaxBandwidth:      stats.MaxBandwidth,
			NetworkInterfaces: stats.NetworkInterfaces,
		},
		GPU:                nonNilMap(stats.GPUData),
		Sensors:            SensorsData{Temperatures: stats.Temperatures},
		Containers:         containerSnapshotRecords(data.Containers),
		ContainerTelemetry: nonNilSlice(data.ContainerTelemetry),
		Connections:        nonNilPointer(data.Connections),
		IRQ:                nonNilSlice(data.IRQs),
	}
}

func nonNilSlice[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

func nonNilMap[K comparable, V any](items map[K]V) map[K]V {
	if items == nil {
		return map[K]V{}
	}
	return items
}

func nonNilPointer[T any](item *T) any {
	if item == nil {
		return *new(T)
	}
	return item
}

func containerSnapshotRecords(items []*containermodel.Stats) []containerSnapshotRecord {
	out := make([]containerSnapshotRecord, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, containerSnapshotRecord{
			ID:        item.Id,
			Name:      item.Name,
			Image:     item.Image,
			Ports:     item.Ports,
			Status:    item.Status,
			Health:    item.Health,
			Cpu:       item.Cpu,
			Mem:       item.Mem,
			Bandwidth: item.Bandwidth,
		})
	}
	return out
}
