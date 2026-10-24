package api

import (
	"encoding/json/jsontext"
	"time"

	containermodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
)

// Metrics plugins whose history the bridge reads.
const (
	PluginCPU                = "cpu"
	PluginMem                = "mem"
	PluginDiskIO             = "diskio"
	PluginNetwork            = "network"
	PluginContainers         = "containers"
	PluginContainerTelemetry = "container_telemetry"
)

var historyResolutions = []string{"1m", "10m", "20m", "120m", "480m"}

var historyResolutionSteps = map[string]time.Duration{
	"1m":   time.Minute,
	"10m":  10 * time.Minute,
	"20m":  20 * time.Minute,
	"120m": 120 * time.Minute,
	"480m": 480 * time.Minute,
}

// HistoryResolutions lists the supported history resolutions, finest first.
func HistoryResolutions() []string {
	return append([]string(nil), historyResolutions...)
}

// HistoryResolutionStep returns the bucket width of a resolution.
func HistoryResolutionStep(resolution string) (time.Duration, bool) {
	step, ok := historyResolutionSteps[resolution]
	return step, ok
}

type HistoryItem struct {
	CapturedAt int64          `json:"captured_at"`
	Stats      jsontext.Value `json:"stats"`
}

// HistoryResponse is the body of RouteHistory; Stats is decoded per plugin
// into one of the *HistoryStats / *Record types below.
type HistoryResponse struct {
	Resolution string        `json:"resolution"`
	Items      []HistoryItem `json:"items"`
}

type CPUHistoryStats struct {
	CPUPercent       float64   `json:"cpu_percent"`
	BreakdownPercent []float64 `json:"cpu_breakdown_percent"`
	CoresPercent     []float64 `json:"cpu_cores_percent"`
	FrequenciesMHz   []float64 `json:"cpu_frequencies_mhz"`
}

type MemHistoryStats struct {
	TotalGB       float64 `json:"memory_gb"`
	UsedGB        float64 `json:"memory_used_gb"`
	UsedPercent   float64 `json:"memory_percent"`
	BufferCacheGB float64 `json:"memory_buffer_cache_gb"`
	ZFSArcGB      float64 `json:"memory_zfs_arc_gb"`
	CachedGB      float64 `json:"memory_cached_gb"`
	BuffersGB     float64 `json:"memory_buffers_gb"`
}

type DiskIOHistoryStats struct {
	// IOBytesPerSec is [read, write].
	IOBytesPerSec [2]float64 `json:"disk_io_bytes_per_second"`
}

type NetworkHistoryStats struct {
	// BandwidthBytesPerSec is [sent, received].
	BandwidthBytesPerSec [2]float64 `json:"bandwidth_bytes_per_second"`
	// NetworkInterfaces values are [sent/s, received/s, total sent, total received].
	NetworkInterfaces map[string][4]float64 `json:"network_interfaces"`
}

// ContainerHistoryRecord is one container's entry in a containers-plugin
// history point, whose stats payload is an array of these records.
type ContainerHistoryRecord struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	CPUPct float64 `json:"cpu_percent"`
	MemMB  float64 `json:"memory_mb"`
	// Bandwidth is [sent, received] bytes per second.
	Bandwidth [2]uint64 `json:"bandwidth_bytes"`
}

// ContainerTelemetryRecord is one container's entry in a
// container_telemetry-plugin history point. That plugin attributes host
// processes to containers, so it carries the block I/O the containers plugin
// does not collect.
type ContainerTelemetryRecord = containermodel.Telemetry
