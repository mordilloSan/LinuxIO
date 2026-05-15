package system

import (
	"context"

	"github.com/shirou/gopsutil/v4/process"
)

type ProcInfo struct {
	Pid     int32   `json:"pid"`
	Name    string  `json:"name"`
	CPU     float64 `json:"cpu_percent"`
	Memory  float32 `json:"mem_percent"`
	Running bool    `json:"running"`
}

func FetchProcesses(ctx context.Context) ([]ProcInfo, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]ProcInfo, 0, len(procs))
	for _, p := range procs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name, _ := p.NameWithContext(ctx)
		cpu, _ := p.CPUPercentWithContext(ctx)
		mem, _ := p.MemoryPercentWithContext(ctx)
		running, _ := p.IsRunningWithContext(ctx)
		result = append(result, ProcInfo{
			Pid: p.Pid, Name: name, CPU: cpu, Memory: mem, Running: running,
		})
	}
	return result, nil
}
