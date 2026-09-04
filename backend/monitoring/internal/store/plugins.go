package store

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

const (
	PluginCPU                = "cpu"
	PluginMem                = "mem"
	PluginSwap               = "swap"
	PluginLoad               = "load"
	PluginDiskIO             = "diskio"
	PluginFS                 = "fs"
	PluginNetwork            = "network"
	PluginGPU                = "gpu"
	PluginSensors            = "sensors"
	PluginContainers         = "containers"
	PluginContainerTelemetry = "container_telemetry"
	PluginProcesses          = "processes"
	PluginPrograms           = "programs"
	PluginConnections        = "connections"
	PluginIRQ                = "irq"
	PluginSmart              = "smart"
)

var pluginNames = []string{
	PluginCPU,
	PluginMem,
	PluginSwap,
	PluginLoad,
	PluginDiskIO,
	PluginFS,
	PluginNetwork,
	PluginGPU,
	PluginSensors,
	PluginContainers,
	PluginContainerTelemetry,
	PluginProcesses,
	PluginPrograms,
	PluginConnections,
	PluginIRQ,
	PluginSmart,
}

var defaultHistoryPluginNames = []string{
	PluginCPU,
	PluginMem,
	PluginSwap,
	PluginDiskIO,
	PluginNetwork,
	PluginContainers,
	PluginContainerTelemetry,
}

var liveOnlyPluginNames = map[string]struct{}{
	PluginProcesses: {},
	PluginPrograms:  {},
}

type CPUData struct {
	Cpu           float64           `json:"cpu_percent"`
	MaxCpu        float64           `json:"max_cpu_percent,omitempty"`
	CpuBreakdown  []float64         `json:"cpu_breakdown_percent,omitempty"`
	CpuCoresUsage system.Uint8Slice `json:"cpu_cores_percent,omitempty"`
}

type MemData struct {
	Mem          float64 `json:"memory_gb"`
	MaxMem       float64 `json:"max_memory_gb,omitempty"`
	MemUsed      float64 `json:"memory_used_gb"`
	MemPct       float64 `json:"memory_percent"`
	MemBuffCache float64 `json:"memory_buffer_cache_gb"`
	MemZfsArc    float64 `json:"memory_zfs_arc_gb,omitempty"`
	MemSwapTotal float64 `json:"memory_swap_total_gb"`
	MemSwapUsed  float64 `json:"memory_swap_used_gb"`
	MemSwapPct   float64 `json:"memory_swap_percent"`
	MemAvailable float64 `json:"memory_available_gb"`
	MemCached    float64 `json:"memory_cached_gb"`
	MemBuffers   float64 `json:"memory_buffers_gb"`
}

type SwapData struct {
	Swap     float64 `json:"swap_gb,omitempty"`
	SwapUsed float64 `json:"swap_used_gb,omitempty"`
}

type LoadData struct {
	LoadAvg [3]float64 `json:"load_average,omitempty"`
	Battery [2]uint8   `json:"battery,omitzero"`
}

type DiskIOData struct {
	DiskTotal      float64    `json:"disk_total_gb"`
	DiskUsed       float64    `json:"disk_used_gb"`
	DiskPct        float64    `json:"disk_percent"`
	DiskReadPs     float64    `json:"disk_read_mb_per_second,omitzero"`
	DiskWritePs    float64    `json:"disk_write_mb_per_second,omitzero"`
	MaxDiskReadPs  float64    `json:"max_disk_read_mb_per_second,omitempty"`
	MaxDiskWritePs float64    `json:"max_disk_write_mb_per_second,omitempty"`
	DiskIO         [2]uint64  `json:"disk_io_bytes_per_second,omitzero"`
	MaxDiskIO      [2]uint64  `json:"max_disk_io_bytes_per_second,omitzero"`
	DiskIoStats    [6]float64 `json:"disk_io_stats,omitzero"`
	MaxDiskIoStats [6]float64 `json:"max_disk_io_stats,omitzero"`
}

type FSData struct {
	ExtraFs map[string]*system.FsStats `json:"extra_filesystems,omitempty"`
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
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Image     string                 `json:"image,omitempty"`
	Ports     string                 `json:"ports,omitempty"`
	Status    string                 `json:"status,omitempty"`
	Health    container.DockerHealth `json:"health,omitempty"`
	Cpu       float64                `json:"cpu_percent"`
	Mem       float64                `json:"memory_mb"`
	Bandwidth [2]uint64              `json:"bandwidth_bytes,omitempty,omitzero"`
}

// PluginNames returns the canonical v1 plugin order.
func PluginNames() []string {
	return slices.Clone(pluginNames)
}

// DefaultHistoryPluginNames returns the default plugin history allowlist.
func DefaultHistoryPluginNames() []string {
	return slices.Clone(defaultHistoryPluginNames)
}

func IsPluginName(name string) bool {
	return slices.Contains(pluginNames, name)
}

// IsLiveOnlyPlugin reports whether a plugin is collected for live responses
// only and is never persisted to the metrics database.
func IsLiveOnlyPlugin(name string) bool {
	_, ok := liveOnlyPluginNames[name]
	return ok
}

func pluginHistoryTable(plugin string) string {
	return plugin + "_history"
}

func historyPluginSet(plugins []string) map[string]struct{} {
	out := make(map[string]struct{}, len(plugins))
	for _, plugin := range plugins {
		if _, liveOnly := liveOnlyPluginNames[plugin]; liveOnly {
			continue
		}
		out[plugin] = struct{}{}
	}
	return out
}

func SnapshotPluginPayloads(data *system.CombinedData) map[string]any {
	stats := data.Stats
	return map[string]any{
		PluginCPU: CPUData{
			Cpu:           stats.Cpu,
			MaxCpu:        stats.MaxCpu,
			CpuBreakdown:  stats.CpuBreakdown,
			CpuCoresUsage: stats.CpuCoresUsage,
		},
		PluginMem: MemData{
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
		PluginSwap: SwapData{
			Swap:     stats.Swap,
			SwapUsed: stats.SwapUsed,
		},
		PluginLoad: LoadData{
			LoadAvg: stats.LoadAvg,
			Battery: stats.Battery,
		},
		PluginDiskIO: DiskIOData{
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
		PluginFS: FSData{
			ExtraFs: stats.ExtraFs,
		},
		PluginNetwork: NetworkData{
			Bandwidth:         stats.Bandwidth,
			MaxBandwidth:      stats.MaxBandwidth,
			NetworkInterfaces: stats.NetworkInterfaces,
		},
		PluginGPU:                nonNilMap(stats.GPUData),
		PluginSensors:            SensorsData{Temperatures: stats.Temperatures},
		PluginContainers:         containerSnapshotRecords(data.Containers),
		PluginContainerTelemetry: nonNilSlice(data.ContainerTelemetry),
		PluginConnections:        nonNilPointer(data.Connections),
		PluginIRQ:                nonNilSlice(data.IRQs),
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

func containerSnapshotRecords(items []*container.Stats) []containerSnapshotRecord {
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

func parseHistoryPlugins(raw string, explicit bool, envValue func(string) (string, bool)) ([]string, error) {
	if !explicit {
		if value, ok := envValue("HISTORY"); ok {
			raw = value
			explicit = true
		}
	}
	if !explicit || strings.TrimSpace(raw) == "" {
		return DefaultHistoryPluginNames(), nil
	}

	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "all":
		return historyCapablePluginNames(), nil
	case "none":
		return nil, nil
	}

	seen := make(map[string]struct{})
	var out []string
	for part := range strings.SplitSeq(normalized, ",") {
		plugin := strings.TrimSpace(part)
		if plugin == "" {
			continue
		}
		if !IsPluginName(plugin) {
			return nil, fmt.Errorf("unknown history plugin %q", plugin)
		}
		if _, liveOnly := liveOnlyPluginNames[plugin]; liveOnly {
			return nil, fmt.Errorf("history is not supported for live-only plugin %q", plugin)
		}
		if _, ok := seen[plugin]; ok {
			continue
		}
		seen[plugin] = struct{}{}
		out = append(out, plugin)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// HistoryEvery converts per-plugin history intervals into a write cadence in
// collector ticks. Every interval must be a whole multiple of the tick; smart
// history follows collector.smart_refresh_interval and live-only plugins have
// no history, so neither accepts an interval.
func HistoryEvery(intervals map[string]time.Duration, tick time.Duration) (map[string]uint64, error) {
	if tick <= 0 {
		return nil, fmt.Errorf("collector interval must be greater than zero")
	}
	out := make(map[string]uint64, len(intervals))
	for plugin, interval := range intervals {
		switch {
		case !IsPluginName(plugin):
			return nil, fmt.Errorf("unknown history plugin %q", plugin)
		case IsLiveOnlyPlugin(plugin):
			return nil, fmt.Errorf("history is not supported for live-only plugin %q", plugin)
		case plugin == PluginSmart:
			return nil, fmt.Errorf("%q history follows collector.smart_refresh_interval", plugin)
		case interval < tick || interval%tick != 0:
			return nil, fmt.Errorf("%s interval %s must be a whole multiple of the collector interval %s", plugin, interval, tick)
		}
		out[plugin] = uint64(interval / tick)
	}
	return out, nil
}

func historyCapablePluginNames() []string {
	out := make([]string, 0, len(pluginNames)-len(liveOnlyPluginNames))
	for _, plugin := range pluginNames {
		if _, liveOnly := liveOnlyPluginNames[plugin]; !liveOnly {
			out = append(out, plugin)
		}
	}
	return out
}
