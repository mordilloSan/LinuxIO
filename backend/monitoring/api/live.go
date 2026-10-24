package api

import (
	processmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/process"
	smartmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/smart"
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
	InodesFree        *uint64  `json:"inodesFree,omitzero"`
	InodesTotal       *uint64  `json:"inodesTotal,omitzero"`
	InodesUsed        *uint64  `json:"inodesUsed,omitzero"`
	InodesUsedPercent *float64 `json:"inodesUsedPercent,omitzero"`
	Mountpoint        string   `json:"mountpoint"`
	ReadOnly          *bool    `json:"readOnly,omitzero"`
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
	ActualFreqMHz          *float64 `json:"actual_freq_mhz,omitzero"`
	BoostFreqMHz           *float64 `json:"boost_freq_mhz,omitzero"`
	ConnectedDisplays      *int     `json:"connected_displays,omitzero"`
	CurrentFreqMHz         *float64 `json:"current_freq_mhz,omitzero"`
	DisplayNames           []string `json:"display_names,omitempty"`
	FanPercent             *float64 `json:"fan_percent,omitzero"`
	FanRPM                 *float64 `json:"fan_rpm,omitzero"`
	GTTTotalBytes          *uint64  `json:"gtt_total_bytes,omitzero"`
	GTTUsedBytes           *uint64  `json:"gtt_used_bytes,omitzero"`
	LinkSpeed              *string  `json:"link_speed,omitzero"`
	LinkWidth              *string  `json:"link_width,omitzero"`
	MaxFreqMHz             *float64 `json:"max_freq_mhz,omitzero"`
	MaxLinkSpeed           *string  `json:"max_link_speed,omitzero"`
	MaxLinkWidth           *string  `json:"max_link_width,omitzero"`
	MemoryFreeBytes        *uint64  `json:"memory_free_bytes,omitzero"`
	MemoryTotalBytes       *uint64  `json:"memory_total_bytes,omitzero"`
	MemoryUsedBytes        *uint64  `json:"memory_used_bytes,omitzero"`
	MinFreqMHz             *float64 `json:"min_freq_mhz,omitzero"`
	PowerDrawWatts         *float64 `json:"power_draw_watts,omitzero"`
	PowerLimitWatts        *float64 `json:"power_limit_watts,omitzero"`
	PowerState             *string  `json:"power_state,omitzero"`
	RC6ResidencyMS         *float64 `json:"rc6_residency_ms,omitzero"`
	RequestedFreqMHz       *float64 `json:"requested_freq_mhz,omitzero"`
	RP0FreqMHz             *float64 `json:"rp0_freq_mhz,omitzero"`
	RP1FreqMHz             *float64 `json:"rp1_freq_mhz,omitzero"`
	RPNFreqMHz             *float64 `json:"rpn_freq_mhz,omitzero"`
	RuntimeStatus          *string  `json:"runtime_status,omitzero"`
	TemperatureC           *float64 `json:"temperature_c,omitzero"`
	UtilizationPercent     *float64 `json:"utilization_percent,omitzero"`
	VisibleMemoryTotalByte *uint64  `json:"visible_memory_total_bytes,omitzero"`
	VisibleMemoryUsedBytes *uint64  `json:"visible_memory_used_bytes,omitzero"`
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
	Power *DiskPowerData `json:"power,omitzero"`
}

// Process, Program, and ProcessCount are aliases of the daemon domain records
// exposed here so bridge clients never need to import monitoring/internal.
type Process = processmodel.Process
type Program = processmodel.Program
type ProcessCount = processmodel.Count

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
	BlockReadBytesPerSec  *float64 `json:"block_read_bytes_per_sec,omitzero"`
	BlockWriteBytesPerSec *float64 `json:"block_write_bytes_per_sec,omitzero"`
}
