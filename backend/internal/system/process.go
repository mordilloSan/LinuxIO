package system

import (
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/process"
)

type ProcInfo struct {
	Pid    int32   `json:"pid"`
	Name   string  `json:"name"`
	CPU    float64 `json:"cpu_percent"`
	Memory float32 `json:"mem_percent"`
}

func FetchProcesses() ([]ProcInfo, error) {
	procs, err := process.Processes()
	if err != nil {
		return nil, err
	}

	var wg sync.WaitGroup
	ch := make(chan ProcInfo, len(procs))

	for _, p := range procs {
		wg.Add(1)
		go func(p *process.Process) {
			defer wg.Done()
			name, _ := p.Name()
			cpu, _ := p.CPUPercent()
			mem, _ := p.MemoryPercent()
			ch <- ProcInfo{Pid: p.Pid, Name: name, CPU: cpu, Memory: mem}
		}(p)
	}

	wg.Wait()
	close(ch)

	var result []ProcInfo
	for info := range ch {
		result = append(result, info)
	}

	return result, nil
}

func getProcesses(c *gin.Context) {
	result, err := FetchProcesses()
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to list processes", "details": err.Error()})
		return
	}
	c.JSON(200, result)
}
