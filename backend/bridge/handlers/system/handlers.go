package system

import (
	"github.com/mordilloSan/LinuxIO/common/ipc"
)

func SystemHandlers() map[string]ipc.HandlerFunc {
	return map[string]ipc.HandlerFunc{
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
	}
}
