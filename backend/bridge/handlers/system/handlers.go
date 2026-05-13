package system

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers all system handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	handlers := systemHandlers{rt: rt}
	bridgeipc.RegisterRoutes(router, "system", []bridgeipc.Command{
		{Name: "get_capabilities", Mode: bridgeipc.ModeQuery, Handler: handleGetCapabilities},
		{Name: "get_cpu_info", Mode: bridgeipc.ModeQuery, Handler: handleGetCPUInfo},
		{Name: "get_sensor_info", Mode: bridgeipc.ModeQuery, Handler: handleGetSensorInfo},
		{Name: "get_motherboard_info", Mode: bridgeipc.ModeQuery, Handler: handleGetMotherboardInfo},
		{Name: "get_memory_info", Mode: bridgeipc.ModeQuery, Handler: handleGetMemoryInfo},
		{Name: "get_host_info", Mode: bridgeipc.ModeQuery, Handler: handleGetHostInfo},
		{Name: "get_uptime", Mode: bridgeipc.ModeQuery, Handler: handleGetUptime},
		{Name: "get_fs_info", Mode: bridgeipc.ModeQuery, Handler: handleGetFilesystemInfo},
		{Name: "get_processes", Mode: bridgeipc.ModeQuery, Handler: handleGetProcesses},
		{Name: "get_services", Mode: bridgeipc.ModeQuery, Handler: handleGetServices},
		{Name: "get_gpu_info", Mode: bridgeipc.ModeQuery, Handler: handleGetGPUInfo},
		{Name: "get_updates_fast", Mode: bridgeipc.ModeQuery, Handler: handleGetUpdatesFast},
		{Name: "get_network_info", Mode: bridgeipc.ModeQuery, Handler: handleGetNetworkInfo},
		{Name: "get_disk_throughput", Mode: bridgeipc.ModeQuery, Handler: handleGetDiskThroughput},
		{Name: "get_system_info", Mode: bridgeipc.ModeQuery, Handler: handleGetSystemInfo},
		{Name: "get_pci_devices", Mode: bridgeipc.ModeQuery, Handler: handleGetPCIDevices},
		{Name: "get_memory_modules", Mode: bridgeipc.ModeQuery, Handler: handleGetMemoryModules},
		{Name: "get_health_summary", Mode: bridgeipc.ModeQuery, Handler: handlers.handleGetHealthSummary},
		{Name: "list_failed_login_events", Mode: bridgeipc.ModeQuery, Handler: handlers.handleListFailedLoginEvents, Privileged: true},
		{Name: "dismiss_unclean_shutdown", Mode: bridgeipc.ModeJob, Handler: handlers.handleDismissUncleanShutdown},
		{Name: "dismiss_failed_login_alert", Mode: bridgeipc.ModeJob, Handler: handlers.handleDismissFailedLoginAlert},
		{Name: "get_server_time", Mode: bridgeipc.ModeQuery, Handler: handleGetServerTime},
		{Name: "get_timezones", Mode: bridgeipc.ModeQuery, Handler: handleGetTimezones},
	})
}

func handleGetCapabilities(ctx context.Context, args []string, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, buildCapabilitiesResponse(ctx), nil)
}

func handleGetCPUInfo(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := FetchCPUInfo()
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetSensorInfo(ctx context.Context, args []string, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, FetchSensorsInfo(), nil)
}

func handleGetMotherboardInfo(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := FetchBaseboardInfo()
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetMemoryInfo(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := FetchMemoryInfo()
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetHostInfo(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := FetchHostInfo()
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetUptime(ctx context.Context, args []string, emit bridgeipc.Events) error {
	uptimeSeconds, err := FetchUptimeSeconds()
	return bridgeipc.EmitResult(emit, uptimeSeconds, err)
}

func handleGetFilesystemInfo(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := FetchFileSystemInfo(parseIncludeAllArg(args))
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetProcesses(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := FetchProcesses()
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetServices(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := FetchServices()
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetGPUInfo(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := FetchGPUInfo()
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetUpdatesFast(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := GetUpdatesFast()
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetNetworkInfo(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := FetchNetworks()
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetDiskThroughput(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := FetchDiskThroughput()
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetSystemInfo(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := FetchSystemInfo()
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetPCIDevices(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := FetchPCIDevices()
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetMemoryModules(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := FetchMemoryModules()
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetServerTime(ctx context.Context, args []string, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, GetCurrentServerTime(), nil)
}

func handleGetTimezones(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := GetTimezones()
	return bridgeipc.EmitResult(emit, result, err)
}

func (h systemHandlers) handleGetHealthSummary(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := GetHealthSummaryForRuntime(h.rt)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h systemHandlers) handleListFailedLoginEvents(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListFailedLoginEventsForRuntime(ctx, h.rt, args)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h systemHandlers) handleDismissUncleanShutdown(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := DismissUncleanShutdownForRuntime(h.rt, args)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h systemHandlers) handleDismissFailedLoginAlert(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := DismissFailedLoginAlertForRuntime(h.rt, args)
	return bridgeipc.EmitResult(emit, result, err)
}
