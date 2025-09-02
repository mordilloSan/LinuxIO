// file: server/system/proc_services.go
package api

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/process"
)

// ---------- Processes ----------

type ProcInfo struct {
	Pid     int32   `json:"pid"`
	Name    string  `json:"name"`
	CPU     float64 `json:"cpu_percent"`
	Memory  float32 `json:"mem_percent"`
	Running bool    `json:"running"`
}

// FetchProcesses returns a snapshot of processes (unprivileged).
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
			running, _ := p.IsRunning()

			ch <- ProcInfo{
				Pid:     p.Pid,
				Name:    name,
				CPU:     cpu,
				Memory:  mem,
				Running: running,
			}
		}(p)
	}

	wg.Wait()
	close(ch)

	result := make([]ProcInfo, 0, len(procs))
	for info := range ch {
		result = append(result, info)
	}
	return result, nil
}

// Gin handler
func getProcesses(c *gin.Context) {
	list, err := FetchProcesses()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list processes", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}
