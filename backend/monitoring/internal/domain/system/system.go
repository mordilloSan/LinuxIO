// Package system is the host-metrics schema: the stats, info and details that
// are persisted and served as JSON. Collector bookkeeping that must not reach
// the wire lives in the embedded Counters and FsTracking types.
package system

import (
	"encoding/json"
	"time"

	containermodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	networkmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/network"
	processmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/process"
)

type Stats struct {
	Cpu            float64             `json:"cpu_percent"`
	MaxCpu         float64             `json:"max_cpu_percent,omitempty"`
	Mem            float64             `json:"memory_gb"`
	MaxMem         float64             `json:"max_memory_gb,omitempty"`
	MemUsed        float64             `json:"memory_used_gb"`
	MemPct         float64             `json:"memory_percent"`
	MemBuffCache   float64             `json:"memory_buffer_cache_gb"`
	MemZfsArc      float64             `json:"memory_zfs_arc_gb,omitempty"` // ZFS ARC memory
	Swap           float64             `json:"swap_gb,omitempty"`
	SwapUsed       float64             `json:"swap_used_gb,omitempty"`
	SwapPct        float64             `json:"memory_swap_percent,omitempty"`
	MemAvailable   float64             `json:"memory_available_gb,omitempty"`
	MemCached      float64             `json:"memory_cached_gb,omitempty"`
	MemBuffers     float64             `json:"memory_buffers_gb,omitempty"`
	DiskTotal      float64             `json:"disk_total_gb"`
	DiskUsed       float64             `json:"disk_used_gb"`
	DiskPct        float64             `json:"disk_percent"`
	DiskReadPs     float64             `json:"disk_read_mb_per_second,omitzero"`
	DiskWritePs    float64             `json:"disk_write_mb_per_second,omitzero"`
	MaxDiskReadPs  float64             `json:"max_disk_read_mb_per_second,omitempty"`
	MaxDiskWritePs float64             `json:"max_disk_write_mb_per_second,omitempty"`
	Temperatures   map[string]float64  `json:"temperatures,omitempty"`
	ExtraFs        map[string]*FsStats `json:"extra_filesystems,omitempty"`
	GPUData        map[string]GPUData  `json:"gpus,omitempty"`
	Bandwidth      [2]uint64           `json:"bandwidth_bytes_per_second,omitzero"`     // [sent bytes, recv bytes]
	MaxBandwidth   [2]uint64           `json:"max_bandwidth_bytes_per_second,omitzero"` // [sent bytes, recv bytes]
	// TODO: remove other load fields in future release in favor of load avg array
	LoadAvg           [3]float64           `json:"load_average,omitempty"`
	Battery           [2]uint8             `json:"battery,omitzero"`                      // [percent, charge state, current]
	NetworkInterfaces map[string][4]uint64 `json:"network_interfaces,omitempty"`          // [upload bytes/s, download bytes/s, total upload bytes, total download bytes]
	DiskIO            [2]uint64            `json:"disk_io_bytes_per_second,omitzero"`     // [read bytes, write bytes]
	MaxDiskIO         [2]uint64            `json:"max_disk_io_bytes_per_second,omitzero"` // [max read bytes, max write bytes]
	CpuBreakdown      []float64            `json:"cpu_breakdown_percent,omitempty"`       // [user, system, iowait, steal, idle]
	CpuCoresUsage     Uint8Slice           `json:"cpu_cores_percent,omitempty"`           // per-core busy usage [CPU0..]
	CpuFrequencies    []float64            `json:"cpu_frequencies_mhz,omitempty"`         // per-core scaling_cur_freq in MHz [CPU0..]
	DiskIoStats       [6]float64           `json:"disk_io_stats,omitzero"`                // [read time %, write time %, io utilization %, r_await ms, w_await ms, weighted io %]
	MaxDiskIoStats    [6]float64           `json:"max_disk_io_stats,omitzero"`            // max values for DiskIoStats
	// Counters are byte-precise views for the LinuxIO live payload. They are
	// never persisted or serialised as plugin data.
	Counters `json:"-"`
}

// Uint8Slice wraps []uint8 so that JSON encodes it as an array of numbers
// instead of a base64 string.
type Uint8Slice []uint8

func (s Uint8Slice) MarshalJSON() ([]byte, error) {
	if s == nil {
		return []byte("null"), nil
	}
	// Convert to wider ints to force array-of-numbers encoding.
	arr := make([]uint16, len(s))
	for i, v := range s {
		arr[i] = uint16(v)
	}
	return json.Marshal(arr)
}

// GPUData is one GPU's figures. Address, Temperature and Count are collector
// bookkeeping (the PCI join key, the live temperature and the sample count
// behind the averages); they are never persisted.
type GPUData struct {
	Name        string             `json:"name"`
	Address     string             `json:"-"`
	Temperature float64            `json:"-"`
	MemoryUsed  float64            `json:"memory_used_mb,omitempty,omitzero"`
	MemoryTotal float64            `json:"memory_total_mb,omitempty,omitzero"`
	Usage       float64            `json:"usage_percent"`
	Power       float64            `json:"power_watts,omitempty"`
	Count       float64            `json:"-"`
	Engines     map[string]float64 `json:"engines,omitempty"`
	PowerPkg    float64            `json:"package_power_watts,omitempty"`
}

type FsStats struct {
	DiskTotal      float64 `json:"disk_total_gb"`
	DiskUsed       float64 `json:"disk_used_gb"`
	DiskReadPs     float64 `json:"disk_read_mb_per_second"`
	DiskWritePs    float64 `json:"disk_write_mb_per_second"`
	MaxDiskReadPS  float64 `json:"max_disk_read_mb_per_second,omitempty"`
	MaxDiskWritePS float64 `json:"max_disk_write_mb_per_second,omitempty"`
	// TODO: remove DiskReadPs and DiskWritePs in future release in favor of DiskReadBytes and DiskWriteBytes
	DiskReadBytes     uint64     `json:"disk_read_bytes_per_second"`
	DiskWriteBytes    uint64     `json:"disk_write_bytes_per_second"`
	MaxDiskReadBytes  uint64     `json:"max_disk_read_bytes_per_second,omitempty"`
	MaxDiskWriteBytes uint64     `json:"max_disk_write_bytes_per_second,omitempty"`
	DiskIoStats       [6]float64 `json:"disk_io_stats,omitzero"`     // [read time %, write time %, io utilization %, r_await ms, w_await ms, weighted io %]
	MaxDiskIoStats    [6]float64 `json:"max_disk_io_stats,omitzero"` // max values for DiskIoStats
	FsTracking        `json:"-"`
}

// FsTracking is the collector bookkeeping of one tracked filesystem: identity,
// the previous I/O counters and the last raw usage reading. It is never
// persisted.
type FsTracking struct {
	Time              time.Time
	Root              bool
	Mountpoint        string
	Name              string
	TotalRead         uint64
	TotalWrite        uint64
	Device            string
	FSType            string
	ReadOnly          bool
	InodesTotal       uint64
	InodesUsed        uint64
	InodesFree        uint64
	InodesUsedPercent float64
	TotalBytes        uint64
	UsedBytes         uint64
	FreeBytes         uint64
	UsedPercent       float64
	UsageAt           time.Time
}

// Core system data that is needed in All Systems table
type Info struct {
	Threads        int                `json:"threads,omitempty"`
	Uptime         uint64             `json:"uptime_seconds"`
	Cpu            float64            `json:"cpu_percent"`
	MemPct         float64            `json:"memory_percent"`
	DiskPct        float64            `json:"disk_percent"`
	AgentVersion   string             `json:"agent_version"`
	GpuPct         float64            `json:"gpu_percent,omitempty"`
	DashboardTemp  float64            `json:"dashboard_temperature_celsius,omitempty"`
	BandwidthBytes uint64             `json:"bandwidth_bytes_per_second"`
	LoadAvg        [3]float64         `json:"load_average,omitempty"`
	ExtraFsPct     map[string]float64 `json:"extra_filesystem_percent,omitempty"`
	Battery        [2]uint8           `json:"battery,omitzero"` // [percent, charge state]
}

// Data that does not change during process lifetime and is not needed in All Systems table
type Details struct {
	Hostname      string        `json:"hostname"`
	Kernel        string        `json:"kernel_version,omitempty"`
	Cores         int           `json:"cores"`
	Threads       int           `json:"threads"`
	CpuModel      string        `json:"cpu_model"`
	OsName        string        `json:"os_name"`
	Arch          string        `json:"architecture"`
	Podman        bool          `json:"podman,omitempty"`
	MemoryTotal   uint64        `json:"memory_total_bytes"`
	SmartInterval time.Duration `json:"smart_refresh_interval_nanoseconds,omitempty"`
}

// Final data structure returned by the collector.
type CombinedData struct {
	Stats              Stats                         `json:"stats"`
	Info               Info                          `json:"info"`
	Containers         []*containermodel.Stats       `json:"containers"`
	Details            *Details                      `json:"details,omitempty"`
	ProcessCount       *processmodel.Count           `json:"process_count,omitempty"`
	Processes          []processmodel.Process        `json:"processes,omitempty"`
	Programs           []processmodel.Program        `json:"programs,omitempty"`
	ContainerTelemetry []containermodel.Telemetry    `json:"container_telemetry,omitempty"`
	Connections        *networkmodel.ConnectionStats `json:"connections,omitempty"`
	IRQs               []networkmodel.IRQStat        `json:"irq,omitempty"`
}
