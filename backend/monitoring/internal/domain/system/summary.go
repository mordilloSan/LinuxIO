package system

// Summary is the compact, hot-path host payload for frequent polling.
type Summary struct {
	Hostname      string             `json:"hostname,omitempty"`
	OSName        string             `json:"os_name,omitempty"`
	Kernel        string             `json:"kernel_version,omitempty"`
	Architecture  string             `json:"architecture,omitempty"`
	Uptime        uint64             `json:"uptime_seconds"`
	CPUPercent    float64            `json:"cpu_percent"`
	LoadAverage   [3]float64         `json:"load_average,omitempty"`
	CPUModel      string             `json:"cpu_model,omitempty"`
	Cores         int                `json:"cores,omitzero"`
	Threads       int                `json:"threads,omitzero"`
	MemoryTotalGB float64            `json:"memory_gb"`
	MemoryUsedGB  float64            `json:"memory_used_gb"`
	MemoryPercent float64            `json:"memory_percent"`
	MemoryBytes   uint64             `json:"memory_total_bytes,omitempty"`
	Temperatures  map[string]float64 `json:"temperatures,omitempty"`
	DiskPercent   float64            `json:"disk_percent,omitempty"`
	GPUPercent    float64            `json:"gpu_percent,omitempty"`
	Battery       [2]uint8           `json:"battery,omitzero"`
	AgentVersion  string             `json:"agent_version,omitempty"`
}

func NewSummary(data *CombinedData) Summary {
	if data == nil {
		return Summary{}
	}

	summary := Summary{
		Uptime:        data.Info.Uptime,
		CPUPercent:    data.Stats.Cpu,
		LoadAverage:   data.Stats.LoadAvg,
		MemoryTotalGB: data.Stats.Mem,
		MemoryUsedGB:  data.Stats.MemUsed,
		MemoryPercent: data.Stats.MemPct,
		Temperatures:  data.Stats.Temperatures,
		DiskPercent:   data.Stats.DiskPct,
		GPUPercent:    data.Info.GpuPct,
		Battery:       data.Info.Battery,
		AgentVersion:  data.Info.AgentVersion,
	}

	if data.Details != nil {
		summary.Hostname = data.Details.Hostname
		summary.OSName = data.Details.OsName
		summary.Kernel = data.Details.Kernel
		summary.Architecture = data.Details.Arch
		summary.CPUModel = data.Details.CpuModel
		summary.Cores = data.Details.Cores
		summary.Threads = data.Details.Threads
		summary.MemoryBytes = data.Details.MemoryTotal
	}

	if summary.Threads == 0 {
		summary.Threads = data.Info.Threads
	}
	if summary.CPUPercent == 0 {
		summary.CPUPercent = data.Info.Cpu
	}
	if summary.MemoryPercent == 0 {
		summary.MemoryPercent = data.Info.MemPct
	}
	if summary.DiskPercent == 0 {
		summary.DiskPercent = data.Info.DiskPct
	}
	if summary.Battery == [2]uint8{} {
		summary.Battery = data.Stats.Battery
	}

	return summary
}
