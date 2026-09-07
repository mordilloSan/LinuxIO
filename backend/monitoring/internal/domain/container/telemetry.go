package container

// Identity is the host-side identity needed to correlate a process cgroup
// with a container returned by the Docker-compatible runtime API.
type Identity struct {
	ID     string
	FullID string
	Name   string
}

// GPUUsage is the aggregate GPU activity attributed to one container on one
// device. A nil UsagePercent means the active collector can identify the
// process and its memory but cannot measure per-process utilization.
type GPUUsage struct {
	Name            string   `json:"name"`
	ProcessCount    int      `json:"process_count"`
	MemoryUsedBytes *uint64  `json:"memory_used_bytes,omitempty"`
	UsagePercent    *float64 `json:"usage_percent,omitempty"`
	EncodePercent   *float64 `json:"encode_percent,omitempty"`
	DecodePercent   *float64 `json:"decode_percent,omitempty"`
	Source          string   `json:"source,omitempty"`
}

// Telemetry is the bounded, container-level historical record. Individual
// process records remain live-only and are never embedded in this payload.
type Telemetry struct {
	ID                      string              `json:"id"`
	Name                    string              `json:"name"`
	ProcessCount            int                 `json:"process_count"`
	CPUPercent              float64             `json:"cpu_percent"`
	DiskReadBytesPerSecond  uint64              `json:"disk_read_bytes_per_second,omitempty"`
	DiskWriteBytesPerSecond uint64              `json:"disk_write_bytes_per_second,omitempty"`
	GPUs                    map[string]GPUUsage `json:"gpus,omitempty"`
}
