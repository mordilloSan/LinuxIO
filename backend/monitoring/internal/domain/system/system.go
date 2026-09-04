package system

// TODO: this is confusing, make common package with common/types common/helpers etc

import (
	"encoding/json"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	modelnet "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/network"
	procmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/process"
)

type Stats struct {
	Cpu            float64             `json:"cpu_percent" cbor:"0,keyasint"`
	MaxCpu         float64             `json:"max_cpu_percent,omitempty" cbor:"-"`
	Mem            float64             `json:"memory_gb" cbor:"2,keyasint"`
	MaxMem         float64             `json:"max_memory_gb,omitempty" cbor:"-"`
	MemUsed        float64             `json:"memory_used_gb" cbor:"3,keyasint"`
	MemPct         float64             `json:"memory_percent" cbor:"4,keyasint"`
	MemBuffCache   float64             `json:"memory_buffer_cache_gb" cbor:"5,keyasint"`
	MemZfsArc      float64             `json:"memory_zfs_arc_gb,omitempty" cbor:"6,keyasint,omitempty"` // ZFS ARC memory
	Swap           float64             `json:"swap_gb,omitempty" cbor:"7,keyasint,omitempty"`
	SwapUsed       float64             `json:"swap_used_gb,omitempty" cbor:"8,keyasint,omitempty"`
	SwapPct        float64             `json:"memory_swap_percent,omitempty" cbor:"36,keyasint,omitempty"`
	MemAvailable   float64             `json:"memory_available_gb,omitempty" cbor:"37,keyasint,omitempty"`
	MemCached      float64             `json:"memory_cached_gb,omitempty" cbor:"38,keyasint,omitempty"`
	MemBuffers     float64             `json:"memory_buffers_gb,omitempty" cbor:"39,keyasint,omitempty"`
	DiskTotal      float64             `json:"disk_total_gb" cbor:"9,keyasint"`
	DiskUsed       float64             `json:"disk_used_gb" cbor:"10,keyasint"`
	DiskPct        float64             `json:"disk_percent" cbor:"11,keyasint"`
	DiskReadPs     float64             `json:"disk_read_mb_per_second,omitzero" cbor:"12,keyasint,omitzero"`
	DiskWritePs    float64             `json:"disk_write_mb_per_second,omitzero" cbor:"13,keyasint,omitzero"`
	MaxDiskReadPs  float64             `json:"max_disk_read_mb_per_second,omitempty" cbor:"-"`
	MaxDiskWritePs float64             `json:"max_disk_write_mb_per_second,omitempty" cbor:"-"`
	Temperatures   map[string]float64  `json:"temperatures,omitempty" cbor:"20,keyasint,omitempty"`
	ExtraFs        map[string]*FsStats `json:"extra_filesystems,omitempty" cbor:"21,keyasint,omitempty"`
	GPUData        map[string]GPUData  `json:"gpus,omitempty" cbor:"22,keyasint,omitempty"`
	Bandwidth      [2]uint64           `json:"bandwidth_bytes_per_second,omitzero" cbor:"26,keyasint,omitzero"` // [sent bytes, recv bytes]
	MaxBandwidth   [2]uint64           `json:"max_bandwidth_bytes_per_second,omitzero" cbor:"-"`                // [sent bytes, recv bytes]
	// TODO: remove other load fields in future release in favor of load avg array
	LoadAvg           [3]float64           `json:"load_average,omitempty" cbor:"28,keyasint"`
	Battery           [2]uint8             `json:"battery,omitzero" cbor:"29,keyasint,omitzero"`                  // [percent, charge state, current]
	NetworkInterfaces map[string][4]uint64 `json:"network_interfaces,omitempty" cbor:"31,keyasint,omitempty"`     // [upload bytes/s, download bytes/s, total upload bytes, total download bytes]
	DiskIO            [2]uint64            `json:"disk_io_bytes_per_second,omitzero" cbor:"32,keyasint,omitzero"` // [read bytes, write bytes]
	MaxDiskIO         [2]uint64            `json:"max_disk_io_bytes_per_second,omitzero" cbor:"-"`                // [max read bytes, max write bytes]
	CpuBreakdown      []float64            `json:"cpu_breakdown_percent,omitempty" cbor:"33,keyasint,omitempty"`  // [user, system, iowait, steal, idle]
	CpuCoresUsage     Uint8Slice           `json:"cpu_cores_percent,omitempty" cbor:"34,keyasint,omitempty"`      // per-core busy usage [CPU0..]
	DiskIoStats       [6]float64           `json:"disk_io_stats,omitzero" cbor:"35,keyasint,omitzero"`            // [read time %, write time %, io utilization %, r_await ms, w_await ms, weighted io %]
	MaxDiskIoStats    [6]float64           `json:"max_disk_io_stats,omitzero" cbor:"-"`                           // max values for DiskIoStats
	// MemoryBytes and DiskDevices are byte-precise views for the LinuxIO live
	// payload. They are never persisted or serialised as plugin data.
	MemoryBytes MemoryBytes                `json:"-" cbor:"-"`
	DiskDevices map[string]DiskDeviceRates `json:"-" cbor:"-"`
}

// MemoryBytes carries the raw memory counters behind the rounded gigabyte
// fields above.
type MemoryBytes struct {
	Total, Used, Available, Free, Cached, Buffers, Shared, SwapTotal, SwapFree, ZFSArc uint64
}

// DiskDeviceRates carries per-block-device I/O rates.
type DiskDeviceRates struct {
	ReadBytesPerSec, WriteBytesPerSec, ReadOpsPerSec, WriteOpsPerSec float64
}

// Uint8Slice wraps []uint8 to customize JSON encoding while keeping CBOR efficient.
// JSON: encodes as array of numbers (avoids base64 string).
// CBOR: falls back to default handling for []uint8 (byte string), keeping payload small.
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

type GPUData struct {
	Name        string             `json:"name" cbor:"0,keyasint"`
	Temperature float64            `json:"-"`
	MemoryUsed  float64            `json:"memory_used_mb,omitempty,omitzero" cbor:"1,keyasint,omitempty,omitzero"`
	MemoryTotal float64            `json:"memory_total_mb,omitempty,omitzero" cbor:"2,keyasint,omitempty,omitzero"`
	Usage       float64            `json:"usage_percent" cbor:"3,keyasint,omitempty"`
	Power       float64            `json:"power_watts,omitempty" cbor:"4,keyasint,omitempty"`
	Count       float64            `json:"-"`
	Engines     map[string]float64 `json:"engines,omitempty" cbor:"5,keyasint,omitempty"`
	PowerPkg    float64            `json:"package_power_watts,omitempty" cbor:"6,keyasint,omitempty"`
}

type FsStats struct {
	Time           time.Time `json:"-"`
	Root           bool      `json:"-"`
	Mountpoint     string    `json:"-"`
	Name           string    `json:"-"`
	DiskTotal      float64   `json:"disk_total_gb" cbor:"0,keyasint"`
	DiskUsed       float64   `json:"disk_used_gb" cbor:"1,keyasint"`
	TotalRead      uint64    `json:"-"`
	TotalWrite     uint64    `json:"-"`
	DiskReadPs     float64   `json:"disk_read_mb_per_second" cbor:"2,keyasint"`
	DiskWritePs    float64   `json:"disk_write_mb_per_second" cbor:"3,keyasint"`
	MaxDiskReadPS  float64   `json:"max_disk_read_mb_per_second,omitempty" cbor:"-"`
	MaxDiskWritePS float64   `json:"max_disk_write_mb_per_second,omitempty" cbor:"-"`
	// TODO: remove DiskReadPs and DiskWritePs in future release in favor of DiskReadBytes and DiskWriteBytes
	DiskReadBytes     uint64     `json:"disk_read_bytes_per_second" cbor:"6,keyasint,omitempty"`
	DiskWriteBytes    uint64     `json:"disk_write_bytes_per_second" cbor:"7,keyasint,omitempty"`
	MaxDiskReadBytes  uint64     `json:"max_disk_read_bytes_per_second,omitempty" cbor:"-"`
	MaxDiskWriteBytes uint64     `json:"max_disk_write_bytes_per_second,omitempty" cbor:"-"`
	DiskIoStats       [6]float64 `json:"disk_io_stats,omitzero" cbor:"8,keyasint,omitzero"` // [read time %, write time %, io utilization %, r_await ms, w_await ms, weighted io %]
	MaxDiskIoStats    [6]float64 `json:"max_disk_io_stats,omitzero" cbor:"-"`               // max values for DiskIoStats
}

type NetIoStats struct {
	BytesRecv uint64
	BytesSent uint64
	Time      time.Time
	Name      string
}

// Core system data that is needed in All Systems table
type Info struct {
	Threads        int                `json:"threads,omitempty" cbor:"3,keyasint,omitempty"`
	Uptime         uint64             `json:"uptime_seconds" cbor:"5,keyasint"`
	Cpu            float64            `json:"cpu_percent" cbor:"6,keyasint"`
	MemPct         float64            `json:"memory_percent" cbor:"7,keyasint"`
	DiskPct        float64            `json:"disk_percent" cbor:"8,keyasint"`
	AgentVersion   string             `json:"agent_version" cbor:"10,keyasint"`
	GpuPct         float64            `json:"gpu_percent,omitempty" cbor:"12,keyasint,omitempty"`
	DashboardTemp  float64            `json:"dashboard_temperature_celsius,omitempty" cbor:"13,keyasint,omitempty"`
	BandwidthBytes uint64             `json:"bandwidth_bytes_per_second" cbor:"18,keyasint"`
	LoadAvg        [3]float64         `json:"load_average,omitempty" cbor:"19,keyasint"`
	ExtraFsPct     map[string]float64 `json:"extra_filesystem_percent,omitempty" cbor:"21,keyasint,omitempty"`
	Battery        [2]uint8           `json:"battery,omitzero" cbor:"23,keyasint,omitzero"` // [percent, charge state]
}

// Data that does not change during process lifetime and is not needed in All Systems table
type Details struct {
	Hostname      string        `json:"hostname" cbor:"0,keyasint"`
	Kernel        string        `json:"kernel_version,omitempty" cbor:"1,keyasint,omitempty"`
	Cores         int           `json:"cores" cbor:"2,keyasint"`
	Threads       int           `json:"threads" cbor:"3,keyasint"`
	CpuModel      string        `json:"cpu_model" cbor:"4,keyasint"`
	OsName        string        `json:"os_name" cbor:"6,keyasint"`
	Arch          string        `json:"architecture" cbor:"7,keyasint"`
	Podman        bool          `json:"podman,omitempty" cbor:"8,keyasint,omitempty"`
	MemoryTotal   uint64        `json:"memory_total_bytes" cbor:"9,keyasint"`
	SmartInterval time.Duration `json:"smart_refresh_interval_nanoseconds,omitempty" cbor:"10,keyasint,omitempty"`
}

// Final data structure returned by the collector.
type CombinedData struct {
	Stats              Stats                     `json:"stats" cbor:"0,keyasint"`
	Info               Info                      `json:"info" cbor:"1,keyasint"`
	Containers         []*container.Stats        `json:"containers" cbor:"2,keyasint"`
	Details            *Details                  `json:"details,omitempty" cbor:"4,keyasint,omitempty"`
	ProcessCount       *procmodel.Count          `json:"process_count,omitempty" cbor:"-"`
	Processes          []procmodel.Process       `json:"processes,omitempty" cbor:"-"`
	Programs           []procmodel.Program       `json:"programs,omitempty" cbor:"-"`
	ContainerTelemetry []container.Telemetry     `json:"container_telemetry,omitempty" cbor:"-"`
	Connections        *modelnet.ConnectionStats `json:"connections,omitempty" cbor:"-"`
	IRQs               []modelnet.IRQStat        `json:"irq,omitempty" cbor:"-"`
}
