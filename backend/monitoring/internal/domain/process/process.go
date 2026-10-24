package process

type MemoryInfo struct {
	RSS    uint64 `json:"rss"`
	VMS    uint64 `json:"vms"`
	HWM    uint64 `json:"hwm,omitzero"`
	Data   uint64 `json:"data,omitzero"`
	Stack  uint64 `json:"stack,omitzero"`
	Locked uint64 `json:"locked,omitzero"`
	Swap   uint64 `json:"swap,omitzero"`
}

type IOCounters struct {
	ReadCount               uint64 `json:"read_count,omitzero"`
	WriteCount              uint64 `json:"write_count,omitzero"`
	ReadBytes               uint64 `json:"read_bytes,omitzero"`
	WriteBytes              uint64 `json:"write_bytes,omitzero"`
	DiskReadBytes           uint64 `json:"disk_read_bytes,omitzero"`
	DiskWriteBytes          uint64 `json:"disk_write_bytes,omitzero"`
	DiskReadBytesPerSecond  uint64 `json:"disk_read_bytes_per_second,omitzero"`
	DiskWriteBytesPerSecond uint64 `json:"disk_write_bytes_per_second,omitzero"`
}

type Process struct {
	PID           int32      `json:"pid"`
	Name          string     `json:"name"`
	Cmdline       []string   `json:"cmdline,omitempty"`
	Username      string     `json:"username,omitempty"`
	Status        string     `json:"status,omitempty"`
	NumThreads    int32      `json:"num_threads,omitzero"`
	CPUPercent    float64    `json:"cpu_percent"`
	MemoryPercent float64    `json:"memory_percent"`
	MemoryInfo    MemoryInfo `json:"memory_info"`
	Nice          int32      `json:"nice,omitzero"`
	CreateTime    int64      `json:"create_time,omitzero"`
	IOCounters    IOCounters `json:"io_counters"`
	ContainerID   string     `json:"container_id,omitempty"`
	ContainerName string     `json:"container_name,omitempty"`
}

type Count struct {
	Total    int `json:"total"`
	Running  int `json:"running"`
	Sleeping int `json:"sleeping"`
	Stopped  int `json:"stopped,omitzero"`
	Zombie   int `json:"zombie,omitzero"`
	Blocked  int `json:"blocked,omitzero"`
	Idle     int `json:"idle,omitzero"`
	Thread   int `json:"thread"`
	PIDMax   int `json:"pid_max,omitzero"`
}

type Program struct {
	Name           string  `json:"name"`
	Count          int     `json:"count"`
	CPUPercent     float64 `json:"cpu_percent"`
	MemoryPercent  float64 `json:"memory_percent"`
	MemoryRSSBytes uint64  `json:"memory_rss_bytes"`
	PIDs           []int32 `json:"pids,omitempty"`
}
