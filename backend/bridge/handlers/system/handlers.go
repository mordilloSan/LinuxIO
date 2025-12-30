package system

func SystemHandlers() map[string]func([]string) (any, error) {
	return map[string]func([]string) (any, error){
		"get_cpu_info": func([]string) (any, error) {
			return FetchCPUInfo()
		},
		"get_sensor_info": func([]string) (any, error) {
			return FetchSensorsInfo(), nil
		},
		"get_motherboard_info": func([]string) (any, error) {
			return FetchBaseboardInfo()
		},
		"get_memory_info": func([]string) (any, error) {
			return FetchMemoryInfo()
		},
		"get_host_info": func([]string) (any, error) {
			return FetchHostInfo()
		},
		"get_uptime": func([]string) (any, error) {
			up, err := FetchUptimeSeconds()
			if err != nil {
				return nil, err
			}
			return map[string]any{"uptime_seconds": up}, nil
		},
		"get_fs_info": func(args []string) (any, error) {
			includeAll := false
			if len(args) > 0 {
				v := args[0]
				if v == "1" || v == "true" || v == "yes" {
					includeAll = true
				}
			}
			return FetchFileSystemInfo(includeAll)
		},
		"get_processes":    func([]string) (any, error) { return FetchProcesses() },
		"get_services":     func([]string) (any, error) { return FetchServices() },
		"get_gpu_info":     func([]string) (any, error) { return FetchGPUInfo() },
		"get_drive_info":   func([]string) (any, error) { return FetchDriveInfoViaSystem() },
		"get_updates_fast": func([]string) (any, error) { return GetUpdatesFast() },
		"get_network_info": func([]string) (any, error) { return FetchNetworks() },
	}
}
