package system

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers all system handlers with the global registry
func RegisterHandlers() {
	registerCapabilitiesHandlers()

	ipc.RegisterFunc("system", "get_cpu_info", func(ctx context.Context, args []string, emit ipc.Events) error {
		cpuInfo, err := FetchCPUInfo()
		if err != nil {
			return err
		}
		return emit.Result(cpuInfo)
	})

	ipc.RegisterFunc("system", "get_sensor_info", func(ctx context.Context, args []string, emit ipc.Events) error {
		sensorInfo := FetchSensorsInfo()
		return emit.Result(sensorInfo)
	})

	ipc.RegisterFunc("system", "get_motherboard_info", func(ctx context.Context, args []string, emit ipc.Events) error {
		motherboardInfo, err := FetchBaseboardInfo()
		if err != nil {
			return err
		}
		return emit.Result(motherboardInfo)
	})

	ipc.RegisterFunc("system", "get_memory_info", func(ctx context.Context, args []string, emit ipc.Events) error {
		memoryInfo, err := FetchMemoryInfo()
		if err != nil {
			return err
		}
		return emit.Result(memoryInfo)
	})

	ipc.RegisterFunc("system", "get_host_info", func(ctx context.Context, args []string, emit ipc.Events) error {
		hostInfo, err := FetchHostInfo()
		if err != nil {
			return err
		}
		return emit.Result(hostInfo)
	})

	ipc.RegisterFunc("system", "get_uptime", func(ctx context.Context, args []string, emit ipc.Events) error {
		up, err := FetchUptimeSeconds()
		if err != nil {
			return err
		}
		return emit.Result(map[string]any{"uptime_seconds": up})
	})

	ipc.RegisterFunc("system", "get_fs_info", func(ctx context.Context, args []string, emit ipc.Events) error {
		includeAll := false
		if len(args) > 0 {
			v := args[0]
			if v == "1" || v == "true" || v == "yes" {
				includeAll = true
			}
		}
		fsInfo, err := FetchFileSystemInfo(includeAll)
		if err != nil {
			return err
		}
		return emit.Result(fsInfo)
	})

	ipc.RegisterFunc("system", "get_processes", func(ctx context.Context, args []string, emit ipc.Events) error {
		processes, err := FetchProcesses()
		if err != nil {
			return err
		}
		return emit.Result(processes)
	})

	ipc.RegisterFunc("system", "get_services", func(ctx context.Context, args []string, emit ipc.Events) error {
		services, err := FetchServices()
		if err != nil {
			return err
		}
		return emit.Result(services)
	})

	ipc.RegisterFunc("system", "get_gpu_info", func(ctx context.Context, args []string, emit ipc.Events) error {
		gpuInfo, err := FetchGPUInfo()
		if err != nil {
			return err
		}
		return emit.Result(gpuInfo)
	})

	ipc.RegisterFunc("system", "get_updates_fast", func(ctx context.Context, args []string, emit ipc.Events) error {
		updates, err := GetUpdatesFast()
		if err != nil {
			return err
		}
		return emit.Result(updates)
	})

	ipc.RegisterFunc("system", "get_network_info", func(ctx context.Context, args []string, emit ipc.Events) error {
		networks, err := FetchNetworks()
		if err != nil {
			return err
		}
		return emit.Result(networks)
	})
}
