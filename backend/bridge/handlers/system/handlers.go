package system

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handler"
)

// RegisterHandlers registers all system handlers with the global registry
func RegisterHandlers() {
	handler.RegisterFunc("system", "get_cpu_info", func(ctx context.Context, args []string, emit handler.Events) error {
		cpuInfo, err := FetchCPUInfo()
		if err != nil {
			return err
		}
		return emit.Result(cpuInfo)
	})

	handler.RegisterFunc("system", "get_sensor_info", func(ctx context.Context, args []string, emit handler.Events) error {
		sensorInfo := FetchSensorsInfo()
		return emit.Result(sensorInfo)
	})

	handler.RegisterFunc("system", "get_motherboard_info", func(ctx context.Context, args []string, emit handler.Events) error {
		motherboardInfo, err := FetchBaseboardInfo()
		if err != nil {
			return err
		}
		return emit.Result(motherboardInfo)
	})

	handler.RegisterFunc("system", "get_memory_info", func(ctx context.Context, args []string, emit handler.Events) error {
		memoryInfo, err := FetchMemoryInfo()
		if err != nil {
			return err
		}
		return emit.Result(memoryInfo)
	})

	handler.RegisterFunc("system", "get_host_info", func(ctx context.Context, args []string, emit handler.Events) error {
		hostInfo, err := FetchHostInfo()
		if err != nil {
			return err
		}
		return emit.Result(hostInfo)
	})

	handler.RegisterFunc("system", "get_uptime", func(ctx context.Context, args []string, emit handler.Events) error {
		up, err := FetchUptimeSeconds()
		if err != nil {
			return err
		}
		return emit.Result(map[string]any{"uptime_seconds": up})
	})

	handler.RegisterFunc("system", "get_fs_info", func(ctx context.Context, args []string, emit handler.Events) error {
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

	handler.RegisterFunc("system", "get_processes", func(ctx context.Context, args []string, emit handler.Events) error {
		processes, err := FetchProcesses()
		if err != nil {
			return err
		}
		return emit.Result(processes)
	})

	handler.RegisterFunc("system", "get_services", func(ctx context.Context, args []string, emit handler.Events) error {
		services, err := FetchServices()
		if err != nil {
			return err
		}
		return emit.Result(services)
	})

	handler.RegisterFunc("system", "get_gpu_info", func(ctx context.Context, args []string, emit handler.Events) error {
		gpuInfo, err := FetchGPUInfo()
		if err != nil {
			return err
		}
		return emit.Result(gpuInfo)
	})

	handler.RegisterFunc("system", "get_drive_info", func(ctx context.Context, args []string, emit handler.Events) error {
		driveInfo, err := FetchDriveInfoViaSystem()
		if err != nil {
			return err
		}
		return emit.Result(driveInfo)
	})

	handler.RegisterFunc("system", "get_updates_fast", func(ctx context.Context, args []string, emit handler.Events) error {
		updates, err := GetUpdatesFast()
		if err != nil {
			return err
		}
		return emit.Result(updates)
	})

	handler.RegisterFunc("system", "get_network_info", func(ctx context.Context, args []string, emit handler.Events) error {
		networks, err := FetchNetworks()
		if err != nil {
			return err
		}
		return emit.Result(networks)
	})
}
