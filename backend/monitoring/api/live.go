package api

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
	Containers    LiveContainers           `json:"containers"`
}

type LiveCPU struct {
	Percent        float64          `json:"percent"`
	PerCorePercent []float64        `json:"per_core_percent"`
	Breakdown      LiveCPUBreakdown `json:"breakdown"`
	LoadAverage    [3]float64       `json:"load_average"`
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
}

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
