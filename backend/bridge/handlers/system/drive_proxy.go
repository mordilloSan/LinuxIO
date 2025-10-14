package system

import (
	bridgedrive "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/drive"
)

// FetchDriveInfoViaSystem proxies to the existing drive handlers implementation.
func FetchDriveInfoViaSystem() ([]map[string]any, error) {
	return bridgedrive.FetchDriveInfo()
}
