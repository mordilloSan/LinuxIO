package system

import (
	"github.com/shirou/gopsutil/v4/process"
)

type ProcInfo struct {
	Pid     int32   `json:"pid"`
	Name    string  `json:"name"`
	CPU     float64 `json:"cpu_percent"`
	Memory  float32 `json:"mem_percent"`
	Running bool    `json:"running"`
}

func FetchProcesses() ([]ProcInfo, error) {
	procs, err := process.Processes()
	if err != nil {
		return nil, err
	}
	result := make([]ProcInfo, 0, len(procs))
	for _, p := range procs {
		name, _ := p.Name()
		cpu, _ := p.CPUPercent()
		mem, _ := p.MemoryPercent()
		running, _ := p.IsRunning()
		result = append(result, ProcInfo{
			Pid: p.Pid, Name: name, CPU: cpu, Memory: mem, Running: running,
		})
	}
	return result, nil
}
