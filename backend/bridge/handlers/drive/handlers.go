package drive

import "fmt"

func DriveHandlers() map[string]func([]string) (any, error) {
	return map[string]func([]string) (any, error){
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
