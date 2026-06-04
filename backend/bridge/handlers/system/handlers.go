package system

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	systemapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers all system handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	handlers := systemHandlers{rt: rt}
	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: systemapi.GetCapabilities, Handle: handleGetCapabilities},
		{Route: systemapi.GetCPUInfo, Handle: handleGetCPUInfo},
		{Route: systemapi.GetSensorInfo, Handle: handleGetSensorInfo},
		{Route: systemapi.GetMotherboardInfo, Handle: handleGetMotherboardInfo},
		{Route: systemapi.GetMemoryInfo, Handle: handleGetMemoryInfo},
		{Route: systemapi.GetHostInfo, Handle: handleGetHostInfo},
		{Route: systemapi.GetUptime, Handle: handleGetUptime},
		{Route: systemapi.GetFsInfo, Handle: handleGetFilesystemInfo},
		{Route: systemapi.GetProcesses, Handle: handleGetProcesses},
		{Route: systemapi.GetServices, Handle: handleGetServices},
		{Route: systemapi.GetGPUInfo, Handle: handleGetGPUInfo},
		{Route: systemapi.GetUpdatesFast, Handle: handleGetUpdatesFast},
		{Route: systemapi.GetNetworkInfo, Handle: handleGetNetworkInfo},
		{Route: systemapi.GetDiskThroughput, Handle: handleGetDiskThroughput},
		{Route: systemapi.GetSystemInfo, Handle: handleGetSystemInfo},
		{Route: systemapi.GetPciDevices, Handle: handleGetPCIDevices},
		{Route: systemapi.GetMemoryModules, Handle: handleGetMemoryModules},
		{Route: systemapi.GetHealthSummary, Handle: handlers.handleGetHealthSummary},
		{Route: systemapi.ListFailedLoginEvents, Handle: handlers.handleListFailedLoginEvents},
		{Route: systemapi.DismissUncleanShutdown, Handle: handlers.handleDismissUncleanShutdown},
		{Route: systemapi.DismissFailedLoginAlert, Handle: handlers.handleDismissFailedLoginAlert},
		{Route: systemapi.GetServerTime, Handle: handleGetServerTime},
		{Route: systemapi.GetTimezones, Handle: handleGetTimezones},
	})
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
