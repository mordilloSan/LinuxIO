package system

import (
	bridgedrive "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/drive"
)

// FetchDriveInfoViaSystem proxies to the existing drive handlers implementation.
func FetchDriveInfoViaSystem() ([]map[string]any, error) {
	return bridgedrive.FetchDriveInfo()
}

// RunSmartTest proxies to the drive package to run SMART self-tests.
func RunSmartTest(device, testType string) (map[string]any, error) {
	return bridgedrive.RunSmartTest(device, testType)
}
