package system

import (
	"context"

	"github.com/shirou/gopsutil/v4/process"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func FetchProcesses(ctx context.Context) ([]apischema.ProcessInfo, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]apischema.ProcessInfo, 0, len(procs))
	for _, p := range procs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		running, _ := p.IsRunningWithContext(ctx)
		result = append(result, apischema.ProcessInfo{Running: running})
	}
	return result, nil
}
