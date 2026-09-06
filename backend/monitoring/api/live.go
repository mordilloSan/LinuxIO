package api

import (
	procmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/process"
	smartmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/smart"
)

const (
	APISocketPath     = "/run/linuxio/monitoring/api.sock"
	ControlSocketPath = "/run/linuxio/monitoring/control.sock"
	RouteLive         = "/api/v1/live"
)

type Live struct {
	CapturedAtMs  int64                    `json:"captured_at_ms"`
	UptimeSeconds uint64                   `json:"uptime_seconds"`
	CPU           LiveCPU                  `json:"cpu"`
	Memory        LiveMemory               `json:"memory"`
	Disks         map[string]LiveDiskRates `json:"disks"`
	DiskIO        LiveDiskRates            `json:"disk_io"`
	Interfaces    map[string]LiveInterface `json:"interfaces"`
	Filesystems   []FilesystemInfo         `json:"filesystems"`
	Sensors       []SensorGroup            `json:"sensors"`
	GPUs          map[string]LiveGPU       `json:"gpus"`
	Smart         map[string]LiveSmart     `json:"smart"`
	Containers    LiveContainers           `json:"containers"`
}

type LiveCPU struct {
	Percent        float64            `json:"percent"`
	PerCorePercent []float64          `json:"per_core_percent"`
	Breakdown      LiveCPUBreakdown   `json:"breakdown"`
	LoadAverage    [3]float64         `json:"load_average"`
	FrequenciesMHz []float64          `json:"frequencies_mhz"`
	Temperatures   map[string]float64 `json:"temperatures"`
}

type LiveCPUBreakdown struct {
	User   float64 `json:"user"`
	System float64 `json:"system"`
	IOWait float64 `json:"iowait"`
	Steal  float64 `json:"steal"`
	Idle   float64 `json:"idle"`
}

type LiveMemory struct {
	TotalBytes      uint64 `json:"total_bytes"`
	UsedBytes       uint64 `json:"used_bytes"`
	AvailableBytes  uint64 `json:"available_bytes"`
	FreeBytes       uint64 `json:"free_bytes"`
	CachedBytes     uint64 `json:"cached_bytes"`
	BuffersBytes    uint64 `json:"buffers_bytes"`
	SharedBytes     uint64 `json:"shared_bytes"`
	SwapTotalBytes  uint64 `json:"swap_total_bytes"`
	SwapFreeBytes   uint64 `json:"swap_free_bytes"`
	ZFSArcBytes     uint64 `json:"zfs_arc_bytes"`
	DockerUsedBytes uint64 `json:"docker_used_bytes"`
}

type LiveDiskRates struct {
	ReadBytesPerSec  float64 `json:"read_bytes_per_sec"`
	WriteBytesPerSec float64 `json:"write_bytes_per_sec"`
	ReadOpsPerSec    float64 `json:"read_ops_per_sec"`
	WriteOpsPerSec   float64 `json:"write_ops_per_sec"`
}

type LiveInterface struct {
	RxBytesPerSec float64 `json:"rx_bytes_per_sec"`
	TxBytesPerSec float64 `json:"tx_bytes_per_sec"`
	RxBytesTotal  uint64  `json:"rx_bytes_total"`
	TxBytesTotal  uint64  `json:"tx_bytes_total"`
	RxDropped     uint64  `json:"rx_dropped"`
	RxErrors      uint64  `json:"rx_errors"`
	RxPackets     uint64  `json:"rx_packets"`
	TxDropped     uint64  `json:"tx_dropped"`
	TxErrors      uint64  `json:"tx_errors"`
	TxPackets     uint64  `json:"tx_packets"`
}

type FilesystemInfo struct {
	Device            string   `json:"device"`
	Free              uint64   `json:"free"`
	FSType            string   `json:"fstype"`
	InodesFree        *uint64  `json:"inodesFree,omitempty"`
	InodesTotal       *uint64  `json:"inodesTotal,omitempty"`
	InodesUsed        *uint64  `json:"inodesUsed,omitempty"`
	InodesUsedPercent *float64 `json:"inodesUsedPercent,omitempty"`
	Mountpoint        string   `json:"mountpoint"`
	ReadOnly          *bool    `json:"readOnly,omitempty"`
	Total             uint64   `json:"total"`
	Used              uint64   `json:"used"`
	UsedPercent       float64  `json:"usedPercent"`
}

type SensorReadingKind string

const (
	SensorReadingKindNumber  SensorReadingKind = "number"
	SensorReadingKindBoolean SensorReadingKind = "boolean"
)

type SensorReading struct {
	Field string            `json:"field,omitempty"`
	Kind  SensorReadingKind `json:"kind"`
	Label string            `json:"label"`
	Unit  string            `json:"unit"`
	Value float64           `json:"value"`
}

type SensorGroup struct {
	Adapter  string          `json:"adapter"`
	Readings []SensorReading `json:"readings"`
}

// LiveGPU contains measurements that change while a GPU is running. The map
// key is the normalized PCI address (or collector device identifier when no
// PCI address is available); static GPU identity remains in the system API.
type LiveGPU struct {
	ActualFreqMHz          *float64 `json:"actual_freq_mhz,omitempty"`
	BoostFreqMHz           *float64 `json:"boost_freq_mhz,omitempty"`
	ConnectedDisplays      *int     `json:"connected_displays,omitempty"`
	CurrentFreqMHz         *float64 `json:"current_freq_mhz,omitempty"`
	DisplayNames           []string `json:"display_names,omitempty"`
	FanPercent             *float64 `json:"fan_percent,omitempty"`
	FanRPM                 *float64 `json:"fan_rpm,omitempty"`
	GTTTotalBytes          *uint64  `json:"gtt_total_bytes,omitempty"`
	GTTUsedBytes           *uint64  `json:"gtt_used_bytes,omitempty"`
	LinkSpeed              *string  `json:"link_speed,omitempty"`
	LinkWidth              *string  `json:"link_width,omitempty"`
	MaxFreqMHz             *float64 `json:"max_freq_mhz,omitempty"`
	MaxLinkSpeed           *string  `json:"max_link_speed,omitempty"`
	MaxLinkWidth           *string  `json:"max_link_width,omitempty"`
	MemoryFreeBytes        *uint64  `json:"memory_free_bytes,omitempty"`
	MemoryTotalBytes       *uint64  `json:"memory_total_bytes,omitempty"`
	MemoryUsedBytes        *uint64  `json:"memory_used_bytes,omitempty"`
	MinFreqMHz             *float64 `json:"min_freq_mhz,omitempty"`
	PowerDrawWatts         *float64 `json:"power_draw_watts,omitempty"`
	PowerLimitWatts        *float64 `json:"power_limit_watts,omitempty"`
	PowerState             *string  `json:"power_state,omitempty"`
	RC6ResidencyMS         *float64 `json:"rc6_residency_ms,omitempty"`
	RequestedFreqMHz       *float64 `json:"requested_freq_mhz,omitempty"`
	RP0FreqMHz             *float64 `json:"rp0_freq_mhz,omitempty"`
	RP1FreqMHz             *float64 `json:"rp1_freq_mhz,omitempty"`
	RPNFreqMHz             *float64 `json:"rpn_freq_mhz,omitempty"`
	RuntimeStatus          *string  `json:"runtime_status,omitempty"`
	TemperatureC           *float64 `json:"temperature_c,omitempty"`
	UtilizationPercent     *float64 `json:"utilization_percent,omitempty"`
	VisibleMemoryTotalByte *uint64  `json:"visible_memory_total_bytes,omitempty"`
	VisibleMemoryUsedBytes *uint64  `json:"visible_memory_used_bytes,omitempty"`
}

type DiskPowerState struct {
	Description string  `json:"description"`
	MaxPowerW   float64 `json:"maxPowerW"`
	State       int     `json:"state"`
}

type DiskPowerData struct {
	CurrentState int              `json:"currentState"`
	EstimatedW   float64          `json:"estimatedW"`
	States       []DiskPowerState `json:"states"`
}

type SmartData = smartmodel.SmartData
type SmartAttribute = smartmodel.SmartAttribute

type LiveSmart struct {
	Data  SmartData      `json:"data"`
	Power *DiskPowerData `json:"power,omitempty"`
}

// Process, Program, and ProcessCount are aliases of the daemon domain records
// exposed here so bridge clients never need to import monitoring/internal.
type Process = procmodel.Process
type Program = procmodel.Program
type ProcessCount = procmodel.Count

type LiveContainers struct {
	CapturedAtMs int64           `json:"captured_at_ms"`
	Items        []LiveContainer `json:"items"`
}

// LiveContainer carries CPU in Docker's multi-core convention (a container
// using two full cores reports 200).
type LiveContainer struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	CPUPercent            float64  `json:"cpu_percent"`
	MemoryBytes           uint64   `json:"memory_bytes"`
	RxBytesPerSec         float64  `json:"rx_bytes_per_sec"`
	TxBytesPerSec         float64  `json:"tx_bytes_per_sec"`
	BlockReadBytesPerSec  *float64 `json:"block_read_bytes_per_sec,omitempty"`
	BlockWriteBytesPerSec *float64 `json:"block_write_bytes_per_sec,omitempty"`
}
