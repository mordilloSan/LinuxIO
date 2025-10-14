package drive

import (
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func DriveHandlers() map[string]ipc.HandlerFunc {
	return map[string]ipc.HandlerFunc{
		"get_drive_info": func([]string) (any, error) {
			return FetchDriveInfo()
		},
		"get_smart_info": func(args []string) (any, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("missing device argument")
			}
			return FetchSmartInfo(args[0])
		},
		"get_nvme_power": func(args []string) (any, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("missing device argument")
			}
			return GetNVMePowerState(args[0])
		},
	}
}
