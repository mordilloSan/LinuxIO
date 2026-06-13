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
		apischema.Query[apischema.NoRequest, apischema.CapabilitiesResponse]("system.get_capabilities").Handle(handleGetCapabilities),
		apischema.Query[apischema.NoRequest, apischema.CPUInfoResponse]("system.get_cpu_info").Handle(handleGetCPUInfo),
		apischema.Query[apischema.NoRequest, []apischema.SensorGroup]("system.get_sensor_info").Handle(handleGetSensorInfo),
		apischema.Query[apischema.NoRequest, apischema.MotherboardInfo]("system.get_motherboard_info").Handle(handleGetMotherboardInfo),
		apischema.Query[apischema.NoRequest, apischema.MemoryInfoResponse]("system.get_memory_info").Handle(handleGetMemoryInfo),
		apischema.Query[apischema.NoRequest, apischema.HostInfo]("system.get_host_info").Handle(handleGetHostInfo),
		apischema.Query[apischema.NoRequest, float64]("system.get_uptime").Handle(handleGetUptime),
		apischema.Query[apischema.NoRequest, []apischema.FilesystemInfo]("system.get_fs_info").Handle(handleGetFilesystemInfo),
		apischema.Query[apischema.NoRequest, []apischema.ProcessInfo]("system.get_processes").Handle(handleGetProcesses),
		apischema.Query[apischema.NoRequest, apischema.NoResponse]("system.get_services", apischema.NoEndpoint()).Handle(handleGetServices),
		apischema.Query[apischema.NoRequest, []apischema.GpuDevice]("system.get_gpu_info").Handle(handleGetGPUInfo),
		apischema.Query[apischema.NoRequest, apischema.UpdatesFastResponse]("system.get_updates_fast").Handle(handleGetUpdatesFast),
		apischema.Query[apischema.NoRequest, []apischema.InterfaceStats]("system.get_network_info").Handle(handleGetNetworkInfo),
		apischema.Query[apischema.NoRequest, apischema.DiskThroughputResponse]("system.get_disk_throughput").Handle(handleGetDiskThroughput),
		apischema.Query[apischema.NoRequest, apischema.SystemInfo]("system.get_system_info").Handle(handleGetSystemInfo),
		apischema.Query[apischema.NoRequest, []apischema.PCIDevice]("system.get_pci_devices").Handle(handleGetPCIDevices),
		apischema.Query[apischema.NoRequest, []apischema.MemoryModule]("system.get_memory_modules").Handle(handleGetMemoryModules),
		apischema.Query[apischema.NoRequest, apischema.SystemHealthSummary]("system.get_health_summary").Handle(handlers.handleGetHealthSummary),
		apischema.Query[apischema.FailedLoginEventsRequest, []apischema.AccountUserLogin]("system.list_failed_login_events", apischema.Privileged()).Handle(handlers.handleListFailedLoginEvents),
		apischema.Job[apischema.BootIDRequest, apischema.MessageResponse]("system.dismiss_unclean_shutdown").Handle(handlers.handleDismissUncleanShutdown),
		apischema.Job[apischema.AlertIDRequest, apischema.MessageResponse]("system.dismiss_failed_login_alert").Handle(handlers.handleDismissFailedLoginAlert),
		apischema.Query[apischema.NoRequest, string]("system.get_server_time").Handle(handleGetServerTime),
		apischema.Query[apischema.NoRequest, []string]("system.get_timezones").Handle(handleGetTimezones),
	)
}

// RegisterHandlers registers all system handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	routeBindings(rt).Register(router)
}

func handleGetCapabilities(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, buildCapabilitiesResponse(ctx), nil)
}

func handleGetCPUInfo(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchCPUInfo(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetSensorInfo(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, FetchSensorsInfo(ctx), nil)
}

func handleGetMotherboardInfo(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchBaseboardInfo(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetMemoryInfo(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchMemoryInfo(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetHostInfo(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchHostInfo(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetUptime(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	uptimeSeconds, err := FetchUptimeSeconds(ctx)
	return bridgeipc.EmitResult(emit, uptimeSeconds, err)
}

func handleGetFilesystemInfo(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchFileSystemInfo(ctx, false)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetProcesses(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchProcesses(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetServices(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchServices(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetGPUInfo(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchGPUInfo(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetUpdatesFast(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := GetUpdatesFast(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetNetworkInfo(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchNetworks(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetDiskThroughput(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchDiskThroughput(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetSystemInfo(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchSystemInfo(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetPCIDevices(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchPCIDevices(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetMemoryModules(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := FetchMemoryModules(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetServerTime(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, GetCurrentServerTime(ctx), nil)
}

func handleGetTimezones(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := GetTimezones(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h systemHandlers) handleGetHealthSummary(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
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
