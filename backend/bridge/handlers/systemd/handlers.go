package systemd

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	systemdapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: systemdapi.ListTimers, Handle: handleListTimers},
		{Route: systemdapi.ListSockets, Handle: handleListSockets},
		{Route: systemdapi.ListServices, Handle: handleListServices},
		{Route: systemdapi.GetUnitInfo, Handle: handleGetUnitInfo},
		{Route: systemdapi.StartService, Handle: handleStartService},
		{Route: systemdapi.StopService, Handle: handleStopService},
		{Route: systemdapi.RestartService, Handle: handleRestartService},
		{Route: systemdapi.ReloadService, Handle: handleReloadService},
		{Route: systemdapi.EnableService, Handle: handleEnableService},
		{Route: systemdapi.DisableService, Handle: handleDisableService},
		{Route: systemdapi.MaskService, Handle: handleMaskService},
		{Route: systemdapi.UnmaskService, Handle: handleUnmaskService},
		{Route: systemdapi.ResetFailedService, Handle: handleResetFailedService},
	})
}

func handleListTimers(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := ListTimers(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleListSockets(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := ListSockets(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleListServices(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := ListServices(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetUnitInfo(ctx context.Context, req apischema.UnitNameRequest, emit bridgeipc.Events) error {
	result, err := GetUnitInfo(ctx, req.UnitName)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleStartService(ctx context.Context, req apischema.ServiceNameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, StartUnit(ctx, req.ServiceName))
}

func handleStopService(ctx context.Context, req apischema.ServiceNameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, StopUnit(ctx, req.ServiceName))
}

func handleRestartService(ctx context.Context, req apischema.ServiceNameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, RestartUnit(ctx, req.ServiceName))
}

func handleReloadService(ctx context.Context, req apischema.ServiceNameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, ReloadUnit(ctx, req.ServiceName))
}

func handleEnableService(ctx context.Context, req apischema.ServiceNameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, EnableUnit(ctx, req.ServiceName))
}

func handleDisableService(ctx context.Context, req apischema.ServiceNameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, DisableUnit(ctx, req.ServiceName))
}

func handleMaskService(ctx context.Context, req apischema.ServiceNameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, MaskUnit(ctx, req.ServiceName))
}

func handleUnmaskService(ctx context.Context, req apischema.ServiceNameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, UnmaskUnit(ctx, req.ServiceName))
}

func handleResetFailedService(ctx context.Context, req apischema.ServiceNameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, ResetFailedUnit(ctx, req.ServiceName))
}
