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
		apischema.Query[apischema.NoRequest, *apischema.CPUInfoResponse]("system.get_cpu_info").Handle(handleGetCPUInfo),
		apischema.Query[apischema.NoRequest, []apischema.SensorGroup]("system.get_sensor_info").Handle(handleGetSensorInfo),
		apischema.Query[apischema.NoRequest, apischema.MotherboardInfo]("system.get_motherboard_info").Handle(handleGetMotherboardInfo),
		apischema.Query[apischema.NoRequest, *apischema.MemoryInfoResponse]("system.get_memory_info").Handle(handleGetMemoryInfo),
		apischema.Query[apischema.NoRequest, apischema.HostInfo]("system.get_host_info").Handle(handleGetHostInfo),
		apischema.Query[apischema.NoRequest, float64]("system.get_uptime").Handle(handleGetUptime),
		apischema.Query[apischema.NoRequest, []apischema.FilesystemInfo]("system.get_fs_info").Handle(handleGetFilesystemInfo),
		apischema.Query[apischema.NoRequest, []apischema.ProcessInfo]("system.get_processes").Handle(handleGetProcesses),
		apischema.Query[apischema.NoRequest, apischema.NoResponse]("system.get_services", apischema.NoEndpoint()).HandleVoid(handleGetServices),
		apischema.Query[apischema.NoRequest, []apischema.GpuDevice]("system.get_gpu_info").Handle(handleGetGPUInfo),
		apischema.Query[apischema.NoRequest, *apischema.UpdatesFastResponse]("system.get_updates_fast").Handle(handleGetUpdatesFast),
		apischema.Query[apischema.NoRequest, []apischema.InterfaceStats]("system.get_network_info").Handle(handleGetNetworkInfo),
		apischema.Query[apischema.NoRequest, apischema.DiskThroughputResponse]("system.get_disk_throughput").Handle(handleGetDiskThroughput),
		apischema.Query[apischema.NoRequest, *apischema.SystemInfo]("system.get_system_info").Handle(handleGetSystemInfo),
		apischema.Query[apischema.NoRequest, []apischema.PCIDevice]("system.get_pci_devices").Handle(handleGetPCIDevices),
		apischema.Query[apischema.NoRequest, []apischema.MemoryModule]("system.get_memory_modules").Handle(handleGetMemoryModules),
		apischema.Query[apischema.NoRequest, *apischema.SystemHealthSummary]("system.get_health_summary").Handle(handlers.handleGetHealthSummary),
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

func handleGetCapabilities(ctx context.Context, _ apischema.NoRequest) (apischema.CapabilitiesResponse, error) {
	return buildCapabilitiesResponse(ctx), nil
}

func handleGetCPUInfo(ctx context.Context, _ apischema.NoRequest) (*apischema.CPUInfoResponse, error) {
	return FetchCPUInfo(ctx)
}

func handleGetSensorInfo(ctx context.Context, _ apischema.NoRequest) ([]apischema.SensorGroup, error) {
	return FetchSensorsInfo(ctx), nil
}

func handleGetMotherboardInfo(ctx context.Context, _ apischema.NoRequest) (apischema.MotherboardInfo, error) {
	result, err := FetchBaseboardInfo(ctx)
	return result, err
}

func handleGetMemoryInfo(ctx context.Context, _ apischema.NoRequest) (*apischema.MemoryInfoResponse, error) {
	return FetchMemoryInfo(ctx)
}

func handleGetHostInfo(ctx context.Context, _ apischema.NoRequest) (apischema.HostInfo, error) {
	result, err := FetchHostInfo(ctx)
	if err != nil {
		return apischema.HostInfo{}, err
	}
	return hostInfoToAPI(result), nil
}

func handleGetUptime(ctx context.Context, _ apischema.NoRequest) (float64, error) {
	uptimeSeconds, err := FetchUptimeSeconds(ctx)
	return float64(uptimeSeconds), err
}

func handleGetFilesystemInfo(ctx context.Context, _ apischema.NoRequest) ([]apischema.FilesystemInfo, error) {
	result, err := FetchFileSystemInfo(ctx, false)
	return result, err
}

func handleGetProcesses(ctx context.Context, _ apischema.NoRequest) ([]apischema.ProcessInfo, error) {
	result, err := FetchProcesses(ctx)
	return result, err
}

func handleGetServices(ctx context.Context, _ apischema.NoRequest) error {
	_, err := FetchServices(ctx)
	return err
}

func handleGetGPUInfo(ctx context.Context, _ apischema.NoRequest) ([]apischema.GpuDevice, error) {
	result, err := FetchGPUInfo(ctx)
	return result, err
}

func handleGetUpdatesFast(ctx context.Context, _ apischema.NoRequest) (*apischema.UpdatesFastResponse, error) {
	return GetUpdatesFast(ctx)
}

func handleGetNetworkInfo(ctx context.Context, _ apischema.NoRequest) ([]apischema.InterfaceStats, error) {
	result, err := FetchNetworks(ctx)
	return result, err
}

func handleGetDiskThroughput(ctx context.Context, _ apischema.NoRequest) (apischema.DiskThroughputResponse, error) {
	result, err := FetchDiskThroughput(ctx)
	return result, err
}

func handleGetSystemInfo(ctx context.Context, _ apischema.NoRequest) (*apischema.SystemInfo, error) {
	return FetchSystemInfo(ctx)
}

func handleGetPCIDevices(ctx context.Context, _ apischema.NoRequest) ([]apischema.PCIDevice, error) {
	result, err := FetchPCIDevices(ctx)
	return result, err
}

func handleGetMemoryModules(ctx context.Context, _ apischema.NoRequest) ([]apischema.MemoryModule, error) {
	result, err := FetchMemoryModules(ctx)
	return result, err
}

func handleGetServerTime(ctx context.Context, _ apischema.NoRequest) (string, error) {
	return GetCurrentServerTime(ctx), nil
}

func handleGetTimezones(ctx context.Context, _ apischema.NoRequest) ([]string, error) {
	result, err := GetTimezones(ctx)
	return result, err
}

func (h systemHandlers) handleGetHealthSummary(ctx context.Context, _ apischema.NoRequest) (*apischema.SystemHealthSummary, error) {
	return GetHealthSummaryForRuntime(ctx, h.rt)
}

func (h systemHandlers) handleListFailedLoginEvents(ctx context.Context, req apischema.FailedLoginEventsRequest) ([]apischema.AccountUserLogin, error) {
	result, err := ListFailedLoginEventsForRuntime(ctx, h.rt, req)
	return result, err
}

func (h systemHandlers) handleDismissUncleanShutdown(ctx context.Context, req apischema.BootIDRequest) (apischema.MessageResponse, error) {
	return DismissUncleanShutdownForRuntime(ctx, h.rt, req)
}

func (h systemHandlers) handleDismissFailedLoginAlert(ctx context.Context, req apischema.AlertIDRequest) (apischema.MessageResponse, error) {
	return DismissFailedLoginAlertForRuntime(ctx, h.rt, req)
}
