package system

import (
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func SystemHandlers() map[string]ipc.HandlerFunc {
	return map[string]ipc.HandlerFunc{
		"get_cpu_info": ipc.WrapSimpleHandler(func([]string) (any, error) {
			return FetchCPUInfo()
		}),
		"get_sensor_info": ipc.WrapSimpleHandler(func([]string) (any, error) {
			return FetchSensorsInfo(), nil
		}),
		"get_motherboard_info": ipc.WrapSimpleHandler(func([]string) (any, error) {
			return FetchBaseboardInfo()
		}),
		"get_memory_info": ipc.WrapSimpleHandler(func([]string) (any, error) {
			return FetchMemoryInfo()
		}),
		"get_host_info": ipc.WrapSimpleHandler(func([]string) (any, error) {
			return FetchHostInfo()
		}),
		"get_uptime": ipc.WrapSimpleHandler(func([]string) (any, error) {
			up, err := FetchUptimeSeconds()
			if err != nil {
				return nil, err
			}
			return map[string]any{"uptime_seconds": up}, nil
		}),
		"get_fs_info": ipc.WrapSimpleHandler(func(args []string) (any, error) {
			includeAll := false
			if len(args) > 0 {
				v := args[0]
				if v == "1" || v == "true" || v == "yes" {
					includeAll = true
				}
			}
			return FetchFileSystemInfo(includeAll)
		}),
		"get_processes":    ipc.WrapSimpleHandler(func([]string) (any, error) { return FetchProcesses() }),
		"get_services":     ipc.WrapSimpleHandler(func([]string) (any, error) { return FetchServices() }),
		"get_gpu_info":     ipc.WrapSimpleHandler(func([]string) (any, error) { return FetchGPUInfo() }),
		"get_drive_info":   ipc.WrapSimpleHandler(func([]string) (any, error) { return FetchDriveInfoViaSystem() }),
		"get_updates_fast": ipc.WrapSimpleHandler(func([]string) (any, error) { return GetUpdatesFast() }),
		"get_network_info": ipc.WrapSimpleHandler(func([]string) (any, error) { return FetchNetworks() }),
	}
}
