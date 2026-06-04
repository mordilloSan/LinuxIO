package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var DismissFailedLoginAlert = routes.Job("system.dismiss_failed_login_alert", apischema.TypeOf[apischema.AlertIDRequest](), apischema.TypeOf[apischema.MessageResponse]())
var DismissUncleanShutdown = routes.Job("system.dismiss_unclean_shutdown", apischema.TypeOf[apischema.BootIDRequest](), apischema.TypeOf[apischema.MessageResponse]())
var GetCapabilities = routes.Query("system.get_capabilities", apischema.NoRequest(), apischema.TypeOf[apischema.CapabilitiesResponse]())
var GetCPUInfo = routes.Query("system.get_cpu_info", apischema.NoRequest(), apischema.TypeOf[apischema.CPUInfoResponse]())
var GetDiskThroughput = routes.Query("system.get_disk_throughput", apischema.NoRequest(), apischema.TypeOf[apischema.DiskThroughputResponse]())
var GetFsInfo = routes.Query("system.get_fs_info", apischema.NoRequest(), apischema.TypeOf[[]apischema.FilesystemInfo]())
var GetGPUInfo = routes.Query("system.get_gpu_info", apischema.NoRequest(), apischema.TypeOf[[]apischema.GpuDevice]())
var GetHealthSummary = routes.Query("system.get_health_summary", apischema.NoRequest(), apischema.TypeOf[apischema.SystemHealthSummary]())
var GetHostInfo = routes.Query("system.get_host_info", apischema.NoRequest(), apischema.TypeOf[apischema.HostInfo]())
var GetMemoryInfo = routes.Query("system.get_memory_info", apischema.NoRequest(), apischema.TypeOf[apischema.MemoryInfoResponse]())
var GetMemoryModules = routes.Query("system.get_memory_modules", apischema.NoRequest(), apischema.TypeOf[[]apischema.MemoryModule]())
var GetMotherboardInfo = routes.Query("system.get_motherboard_info", apischema.NoRequest(), apischema.TypeOf[apischema.MotherboardInfo]())
var GetNetworkInfo = routes.Query("system.get_network_info", apischema.NoRequest(), apischema.TypeOf[[]apischema.InterfaceStats]())
var GetPciDevices = routes.Query("system.get_pci_devices", apischema.NoRequest(), apischema.TypeOf[[]apischema.PCIDevice]())
var GetProcesses = routes.Query("system.get_processes", apischema.NoRequest(), apischema.TypeOf[[]apischema.ProcessInfo]())
var GetSensorInfo = routes.Query("system.get_sensor_info", apischema.NoRequest(), apischema.TypeOf[[]apischema.SensorGroup]())
var GetServerTime = routes.Query("system.get_server_time", apischema.NoRequest(), apischema.TypeOf[string]())
var GetServices = routes.Query("system.get_services", apischema.NoRequest(), apischema.NoResponse(), apischema.NoEndpoint())
var GetSystemInfo = routes.Query("system.get_system_info", apischema.NoRequest(), apischema.TypeOf[apischema.SystemInfo]())
var GetTimezones = routes.Query("system.get_timezones", apischema.NoRequest(), apischema.TypeOf[[]string]())
var GetUpdatesFast = routes.Query("system.get_updates_fast", apischema.NoRequest(), apischema.TypeOf[apischema.UpdatesFastResponse]())
var GetUptime = routes.Query("system.get_uptime", apischema.NoRequest(), apischema.TypeOf[float64]())
var InstallCapability = routes.Runner("system.install_capability", apischema.TypeOf[apischema.CapabilityRequest](), apischema.TypeOf[apischema.JobSnapshot](), apischema.Privileged())
var ListFailedLoginEvents = routes.Query("system.list_failed_login_events", apischema.TypeOf[apischema.FailedLoginEventsRequest](), apischema.TypeOf[[]apischema.AccountUserLogin](), apischema.Privileged())

var Routes = routes.All()
