package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var DismissFailedLoginAlert = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.dismiss_failed_login_alert", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.AlertIDRequest](), Result: apischema.TypeOf[apischema.MessageResponse]()}
var DismissUncleanShutdown = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.dismiss_unclean_shutdown", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.BootIDRequest](), Result: apischema.TypeOf[apischema.MessageResponse]()}
var GetCapabilities = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_capabilities", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.CapabilitiesResponse]()}
var GetCPUInfo = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_cpu_info", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.CPUInfoResponse]()}
var GetDiskThroughput = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_disk_throughput", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.DiskThroughputResponse]()}
var GetFsInfo = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_fs_info", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.FilesystemInfo]()}
var GetGPUInfo = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_gpu_info", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.GpuDevice]()}
var GetHealthSummary = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_health_summary", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.SystemHealthSummary]()}
var GetHostInfo = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_host_info", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.HostInfo]()}
var GetMemoryInfo = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_memory_info", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.MemoryInfoResponse]()}
var GetMemoryModules = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_memory_modules", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.MemoryModule]()}
var GetMotherboardInfo = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_motherboard_info", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.MotherboardInfo]()}
var GetNetworkInfo = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_network_info", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.InterfaceStats]()}
var GetPciDevices = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_pci_devices", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.PCIDevice]()}
var GetProcesses = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_processes", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.ProcessInfo]()}
var GetSensorInfo = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_sensor_info", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.SensorGroup]()}
var GetServerTime = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_server_time", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[string]()}
var GetServices = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_services", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.NoResponse(), NoEndpoint: true}
var GetSystemInfo = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_system_info", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.SystemInfo]()}
var GetTimezones = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_timezones", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]string]()}
var GetUpdatesFast = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_updates_fast", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.UpdatesFastResponse]()}
var GetUptime = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.get_uptime", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[float64]()}
var InstallCapability = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "system.install_capability", Privileged: true, Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.CapabilityRequest](), Result: apischema.TypeOf[apischema.JobSnapshot]()}
var ListFailedLoginEvents = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "system.list_failed_login_events", Privileged: true, Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.FailedLoginEventsRequest](), Result: apischema.TypeOf[[]apischema.AccountUserLogin]()}

var Routes = []apischema.RouteSpec{
	DismissFailedLoginAlert,
	DismissUncleanShutdown,
	GetCapabilities,
	GetCPUInfo,
	GetDiskThroughput,
	GetFsInfo,
	GetGPUInfo,
	GetHealthSummary,
	GetHostInfo,
	GetMemoryInfo,
	GetMemoryModules,
	GetMotherboardInfo,
	GetNetworkInfo,
	GetPciDevices,
	GetProcesses,
	GetSensorInfo,
	GetServerTime,
	GetServices,
	GetSystemInfo,
	GetTimezones,
	GetUpdatesFast,
	GetUptime,
	InstallCapability,
	ListFailedLoginEvents,
}
