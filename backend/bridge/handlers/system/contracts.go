package system

import (
	"github.com/shirou/gopsutil/v4/host"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// host.InfoStat intentionally has a much broader, provider-specific shape.
// Keep it internal and publish only the stable API subset.
func hostInfoToAPI(value *host.InfoStat) apischema.HostInfo {
	if value == nil {
		return apischema.HostInfo{}
	}
	return apischema.HostInfo{
		Hostname: value.Hostname, KernelArch: value.KernelArch, KernelVersion: value.KernelVersion,
		OS: value.OS, Platform: value.Platform, PlatformVersion: value.PlatformVersion,
	}
}
