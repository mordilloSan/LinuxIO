package system

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var Routes = routeBindings(runtime.Runtime{}).Routes()

func routeBindings(rt runtime.Runtime) apischema.BindingSet {
	handlers := systemHandlers{rt: rt}
	return apischema.Bindings(
		apischema.Query("system.get_capabilities", apischema.NoRequest(), apischema.TypeOf[apischema.CapabilitiesResponse]()).Handle(handleGetCapabilities),
		apischema.Query("system.get_cpu_info", apischema.NoRequest(), apischema.TypeOf[apischema.CPUInfoResponse]()).Handle(handleGetCPUInfo),
		apischema.Query("system.get_sensor_info", apischema.NoRequest(), apischema.TypeOf[[]apischema.SensorGroup]()).Handle(handleGetSensorInfo),
		apischema.Query("system.get_motherboard_info", apischema.NoRequest(), apischema.TypeOf[apischema.MotherboardInfo]()).Handle(handleGetMotherboardInfo),
		apischema.Query("system.get_memory_info", apischema.NoRequest(), apischema.TypeOf[apischema.MemoryInfoResponse]()).Handle(handleGetMemoryInfo),
		apischema.Query("system.get_host_info", apischema.NoRequest(), apischema.TypeOf[apischema.HostInfo]()).Handle(handleGetHostInfo),
		apischema.Query("system.get_uptime", apischema.NoRequest(), apischema.TypeOf[float64]()).Handle(handleGetUptime),
		apischema.Query("system.get_fs_info", apischema.NoRequest(), apischema.TypeOf[[]apischema.FilesystemInfo]()).Handle(handleGetFilesystemInfo),
		apischema.Query("system.get_processes", apischema.NoRequest(), apischema.TypeOf[[]apischema.ProcessInfo]()).Handle(handleGetProcesses),
		apischema.Query("system.get_services", apischema.NoRequest(), apischema.NoResponse(), apischema.NoEndpoint()).Handle(handleGetServices),
		apischema.Query("system.get_gpu_info", apischema.NoRequest(), apischema.TypeOf[[]apischema.GpuDevice]()).Handle(handleGetGPUInfo),
		apischema.Query("system.get_updates_fast", apischema.NoRequest(), apischema.TypeOf[apischema.UpdatesFastResponse]()).Handle(handleGetUpdatesFast),
		apischema.Query("system.get_network_info", apischema.NoRequest(), apischema.TypeOf[[]apischema.InterfaceStats]()).Handle(handleGetNetworkInfo),
		apischema.Query("system.get_disk_throughput", apischema.NoRequest(), apischema.TypeOf[apischema.DiskThroughputResponse]()).Handle(handleGetDiskThroughput),
		apischema.Query("system.get_system_info", apischema.NoRequest(), apischema.TypeOf[apischema.SystemInfo]()).Handle(handleGetSystemInfo),
		apischema.Query("system.get_pci_devices", apischema.NoRequest(), apischema.TypeOf[[]apischema.PCIDevice]()).Handle(handleGetPCIDevices),
		apischema.Query("system.get_memory_modules", apischema.NoRequest(), apischema.TypeOf[[]apischema.MemoryModule]()).Handle(handleGetMemoryModules),
		apischema.Query("system.get_health_summary", apischema.NoRequest(), apischema.TypeOf[apischema.SystemHealthSummary]()).Handle(handlers.handleGetHealthSummary),
		apischema.Query("system.list_failed_login_events", apischema.TypeOf[apischema.FailedLoginEventsRequest](), apischema.TypeOf[[]apischema.AccountUserLogin](), apischema.Privileged()).Handle(handlers.handleListFailedLoginEvents),
		apischema.Job("system.dismiss_unclean_shutdown", apischema.TypeOf[apischema.BootIDRequest](), apischema.TypeOf[apischema.MessageResponse]()).Handle(handlers.handleDismissUncleanShutdown),
		apischema.Job("system.dismiss_failed_login_alert", apischema.TypeOf[apischema.AlertIDRequest](), apischema.TypeOf[apischema.MessageResponse]()).Handle(handlers.handleDismissFailedLoginAlert),
		apischema.Query("system.get_server_time", apischema.NoRequest(), apischema.TypeOf[string]()).Handle(handleGetServerTime),
		apischema.Query("system.get_timezones", apischema.NoRequest(), apischema.TypeOf[[]string]()).Handle(handleGetTimezones),
	)
}

// RegisterHandlers registers all system handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	routeBindings(rt).Register(router)
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
