package process

type MemoryInfo struct {
	RSS    uint64 `json:"rss"`
	VMS    uint64 `json:"vms"`
	HWM    uint64 `json:"hwm,omitempty"`
	Data   uint64 `json:"data,omitempty"`
	Stack  uint64 `json:"stack,omitempty"`
	Locked uint64 `json:"locked,omitempty"`
	Swap   uint64 `json:"swap,omitempty"`
}

type IOCounters struct {
	ReadCount               uint64 `json:"read_count,omitempty"`
	WriteCount              uint64 `json:"write_count,omitempty"`
	ReadBytes               uint64 `json:"read_bytes,omitempty"`
	WriteBytes              uint64 `json:"write_bytes,omitempty"`
	DiskReadBytes           uint64 `json:"disk_read_bytes,omitempty"`
	DiskWriteBytes          uint64 `json:"disk_write_bytes,omitempty"`
	DiskReadBytesPerSecond  uint64 `json:"disk_read_bytes_per_second,omitempty"`
	DiskWriteBytesPerSecond uint64 `json:"disk_write_bytes_per_second,omitempty"`
}

type Process struct {
	PID           int32      `json:"pid"`
	Name          string     `json:"name"`
	Cmdline       []string   `json:"cmdline,omitempty"`
	Username      string     `json:"username,omitempty"`
	Status        string     `json:"status,omitempty"`
	NumThreads    int32      `json:"num_threads,omitempty"`
	CPUPercent    float64    `json:"cpu_percent"`
	MemoryPercent float64    `json:"memory_percent"`
	MemoryInfo    MemoryInfo `json:"memory_info"`
	Nice          int32      `json:"nice,omitempty"`
	CreateTime    int64      `json:"create_time,omitempty"`
	IOCounters    IOCounters `json:"io_counters"`
	ContainerID   string     `json:"container_id,omitempty"`
	ContainerName string     `json:"container_name,omitempty"`
}

type Count struct {
	Total    int `json:"total"`
	Running  int `json:"running"`
	Sleeping int `json:"sleeping"`
	Stopped  int `json:"stopped,omitempty"`
	Zombie   int `json:"zombie,omitempty"`
	Blocked  int `json:"blocked,omitempty"`
	Idle     int `json:"idle,omitempty"`
	Thread   int `json:"thread"`
	PIDMax   int `json:"pid_max,omitempty"`
}

type Program struct {
	Name           string  `json:"name"`
	Count          int     `json:"count"`
	CPUPercent     float64 `json:"cpu_percent"`
	MemoryPercent  float64 `json:"memory_percent"`
	MemoryRSSBytes uint64  `json:"memory_rss_bytes"`
	PIDs           []int32 `json:"pids,omitempty"`
}
