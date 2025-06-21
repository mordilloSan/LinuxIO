package system

import (
	"fmt"
	"go-backend/cmd/bridge/handlers/types"
)

func SystemHandlers() map[string]types.HandlerFunc {
	return map[string]types.HandlerFunc{
		"get_drive_info": func(args []string) (any, error) {
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
