package system

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routes = apischema.NewRouteCatalog()

var RouteDismissFailedLoginAlert = routes.Job("system.dismiss_failed_login_alert", apischema.TypeOf[apischema.AlertIDRequest](), apischema.TypeOf[apischema.MessageResponse]())
var RouteDismissUncleanShutdown = routes.Job("system.dismiss_unclean_shutdown", apischema.TypeOf[apischema.BootIDRequest](), apischema.TypeOf[apischema.MessageResponse]())
var RouteGetCapabilities = routes.Query("system.get_capabilities", apischema.NoRequest(), apischema.TypeOf[apischema.CapabilitiesResponse]())
var RouteGetCPUInfo = routes.Query("system.get_cpu_info", apischema.NoRequest(), apischema.TypeOf[apischema.CPUInfoResponse]())
var RouteGetDiskThroughput = routes.Query("system.get_disk_throughput", apischema.NoRequest(), apischema.TypeOf[apischema.DiskThroughputResponse]())
var RouteGetFsInfo = routes.Query("system.get_fs_info", apischema.NoRequest(), apischema.TypeOf[[]apischema.FilesystemInfo]())
var RouteGetGPUInfo = routes.Query("system.get_gpu_info", apischema.NoRequest(), apischema.TypeOf[[]apischema.GpuDevice]())
var RouteGetHealthSummary = routes.Query("system.get_health_summary", apischema.NoRequest(), apischema.TypeOf[apischema.SystemHealthSummary]())
var RouteGetHostInfo = routes.Query("system.get_host_info", apischema.NoRequest(), apischema.TypeOf[apischema.HostInfo]())
var RouteGetMemoryInfo = routes.Query("system.get_memory_info", apischema.NoRequest(), apischema.TypeOf[apischema.MemoryInfoResponse]())
var RouteGetMemoryModules = routes.Query("system.get_memory_modules", apischema.NoRequest(), apischema.TypeOf[[]apischema.MemoryModule]())
var RouteGetMotherboardInfo = routes.Query("system.get_motherboard_info", apischema.NoRequest(), apischema.TypeOf[apischema.MotherboardInfo]())
var RouteGetNetworkInfo = routes.Query("system.get_network_info", apischema.NoRequest(), apischema.TypeOf[[]apischema.InterfaceStats]())
var RouteGetPciDevices = routes.Query("system.get_pci_devices", apischema.NoRequest(), apischema.TypeOf[[]apischema.PCIDevice]())
var RouteGetProcesses = routes.Query("system.get_processes", apischema.NoRequest(), apischema.TypeOf[[]apischema.ProcessInfo]())
var RouteGetSensorInfo = routes.Query("system.get_sensor_info", apischema.NoRequest(), apischema.TypeOf[[]apischema.SensorGroup]())
var RouteGetServerTime = routes.Query("system.get_server_time", apischema.NoRequest(), apischema.TypeOf[string]())
var RouteGetServices = routes.Query("system.get_services", apischema.NoRequest(), apischema.NoResponse(), apischema.NoEndpoint())
var RouteGetSystemInfo = routes.Query("system.get_system_info", apischema.NoRequest(), apischema.TypeOf[apischema.SystemInfo]())
var RouteGetTimezones = routes.Query("system.get_timezones", apischema.NoRequest(), apischema.TypeOf[[]string]())
var RouteGetUpdatesFast = routes.Query("system.get_updates_fast", apischema.NoRequest(), apischema.TypeOf[apischema.UpdatesFastResponse]())
var RouteGetUptime = routes.Query("system.get_uptime", apischema.NoRequest(), apischema.TypeOf[float64]())
var RouteInstallCapability = routes.Runner("system.install_capability", apischema.TypeOf[apischema.CapabilityRequest](), apischema.TypeOf[apischema.JobSnapshot](), apischema.Privileged())
var RouteListFailedLoginEvents = routes.Query("system.list_failed_login_events", apischema.TypeOf[apischema.FailedLoginEventsRequest](), apischema.TypeOf[[]apischema.AccountUserLogin](), apischema.Privileged())

var Routes = routes.All()

// RegisterHandlers registers all system handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	handlers := systemHandlers{rt: rt}
	apischema.RegisterRoutes(router,
		RouteGetCapabilities.Handle(handleGetCapabilities),
		RouteGetCPUInfo.Handle(handleGetCPUInfo),
		RouteGetSensorInfo.Handle(handleGetSensorInfo),
		RouteGetMotherboardInfo.Handle(handleGetMotherboardInfo),
		RouteGetMemoryInfo.Handle(handleGetMemoryInfo),
		RouteGetHostInfo.Handle(handleGetHostInfo),
		RouteGetUptime.Handle(handleGetUptime),
		RouteGetFsInfo.Handle(handleGetFilesystemInfo),
		RouteGetProcesses.Handle(handleGetProcesses),
		RouteGetServices.Handle(handleGetServices),
		RouteGetGPUInfo.Handle(handleGetGPUInfo),
		RouteGetUpdatesFast.Handle(handleGetUpdatesFast),
		RouteGetNetworkInfo.Handle(handleGetNetworkInfo),
		RouteGetDiskThroughput.Handle(handleGetDiskThroughput),
		RouteGetSystemInfo.Handle(handleGetSystemInfo),
		RouteGetPciDevices.Handle(handleGetPCIDevices),
		RouteGetMemoryModules.Handle(handleGetMemoryModules),
		RouteGetHealthSummary.Handle(handlers.handleGetHealthSummary),
		RouteListFailedLoginEvents.Handle(handlers.handleListFailedLoginEvents),
		RouteDismissUncleanShutdown.Handle(handlers.handleDismissUncleanShutdown),
		RouteDismissFailedLoginAlert.Handle(handlers.handleDismissFailedLoginAlert),
		RouteGetServerTime.Handle(handleGetServerTime),
		RouteGetTimezones.Handle(handleGetTimezones),
	)
}

func handleGetCapabilities(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, buildCapabilitiesResponse(ctx), nil)
}

func handleGetCPUInfo(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchCPUInfo(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetSensorInfo(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, FetchSensorsInfo(ctx), nil)
}

func handleGetMotherboardInfo(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchBaseboardInfo(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetMemoryInfo(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchMemoryInfo(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetHostInfo(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchHostInfo(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetUptime(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	uptimeSeconds, err := FetchUptimeSeconds(ctx)
	return bridgeipc.EmitResult(emit, uptimeSeconds, err)
}

func handleGetFilesystemInfo(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchFileSystemInfo(ctx, false)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetProcesses(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchProcesses(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetServices(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchServices(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetGPUInfo(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchGPUInfo(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetUpdatesFast(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := GetUpdatesFast(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetNetworkInfo(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchNetworks(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetDiskThroughput(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchDiskThroughput(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetSystemInfo(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchSystemInfo(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetPCIDevices(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchPCIDevices(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetMemoryModules(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchMemoryModules(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetServerTime(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, GetCurrentServerTime(ctx), nil)
}

func handleGetTimezones(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := GetTimezones(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h systemHandlers) handleGetHealthSummary(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := GetHealthSummaryForRuntime(ctx, h.rt)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h systemHandlers) handleListFailedLoginEvents(ctx context.Context, req apischema.FailedLoginEventsRequest, emit bridgeipc.Events) error {
	result, err := ListFailedLoginEventsForRuntime(ctx, h.rt, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h systemHandlers) handleDismissUncleanShutdown(ctx context.Context, req apischema.BootIDRequest, emit bridgeipc.Events) error {
	result, err := DismissUncleanShutdownForRuntime(ctx, h.rt, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h systemHandlers) handleDismissFailedLoginAlert(ctx context.Context, req apischema.AlertIDRequest, emit bridgeipc.Events) error {
	result, err := DismissFailedLoginAlertForRuntime(ctx, h.rt, req)
	return bridgeipc.EmitResult(emit, result, err)
}
