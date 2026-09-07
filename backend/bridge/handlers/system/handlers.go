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
		apischema.Call[apischema.NoRequest, apischema.CapabilitiesResponse]("system.get_capabilities", apischema.RetrySafe()).Handle(handleGetCapabilities),
		apischema.Call[apischema.NoRequest, *apischema.CPUInfoResponse]("system.get_cpu_info", apischema.RetrySafe()).Handle(handleGetCPUInfo),
		apischema.Call[apischema.NoRequest, apischema.MotherboardInfo]("system.get_motherboard_info", apischema.RetrySafe()).Handle(handleGetMotherboardInfo),
		apischema.Call[apischema.NoRequest, apischema.HostInfo]("system.get_host_info", apischema.RetrySafe()).Handle(handleGetHostInfo),
		apischema.Call[apischema.NoRequest, []apischema.GpuDevice]("system.get_gpu_info", apischema.RetrySafe()).Handle(handleGetGPUInfo),
		apischema.Call[apischema.NoRequest, *apischema.UpdatesFastResponse]("system.get_updates_fast").Handle(handleGetUpdatesFast),
		apischema.Call[apischema.NoRequest, *apischema.SystemInfo]("system.get_system_info", apischema.RetrySafe()).Handle(handleGetSystemInfo),
		apischema.Call[apischema.NoRequest, []apischema.PCIDevice]("system.get_pci_devices", apischema.RetrySafe()).Handle(handleGetPCIDevices),
		apischema.Call[apischema.NoRequest, []apischema.MemoryModule]("system.get_memory_modules", apischema.RetrySafe()).Handle(handleGetMemoryModules),
		apischema.Call[apischema.NoRequest, *apischema.SystemHealthSummary]("system.get_health_summary").Handle(handlers.handleGetHealthSummary),
		apischema.Call[apischema.FailedLoginEventsRequest, []apischema.AccountUserLogin]("system.list_failed_login_events", apischema.RetrySafe(), apischema.Privileged()).Handle(handlers.handleListFailedLoginEvents),
		apischema.Call[apischema.BootIDRequest, apischema.MessageResponse]("system.dismiss_unclean_shutdown").Handle(handlers.handleDismissUncleanShutdown),
		apischema.Call[apischema.AlertIDRequest, apischema.MessageResponse]("system.dismiss_failed_login_alert").Handle(handlers.handleDismissFailedLoginAlert),
		apischema.Call[apischema.NoRequest, string]("system.get_server_time", apischema.RetrySafe()).Handle(handleGetServerTime),
		apischema.Call[apischema.NoRequest, []string]("system.get_timezones", apischema.RetrySafe()).Handle(handleGetTimezones),
	)
}

// RegisterHandlers registers all system handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	routeBindings(rt).Register(router)
}

func handleGetCapabilities(ctx context.Context, _ apischema.NoRequest) (apischema.CapabilitiesResponse, error) {
	return buildCapabilitiesResponse(ctx)
}

func handleGetCPUInfo(ctx context.Context, _ apischema.NoRequest) (*apischema.CPUInfoResponse, error) {
	return FetchCPUInfo(ctx)
}

func handleGetMotherboardInfo(ctx context.Context, _ apischema.NoRequest) (apischema.MotherboardInfo, error) {
	return FetchBaseboardInfo(ctx)
}

func handleGetHostInfo(ctx context.Context, _ apischema.NoRequest) (apischema.HostInfo, error) {
	return FetchHostInfo(ctx)
}

func handleGetGPUInfo(ctx context.Context, _ apischema.NoRequest) ([]apischema.GpuDevice, error) {
	return FetchGPUInfo(ctx)
}

func handleGetUpdatesFast(ctx context.Context, _ apischema.NoRequest) (*apischema.UpdatesFastResponse, error) {
	return GetUpdatesFast(ctx)
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
	return GetCurrentServerTime(ctx)
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
