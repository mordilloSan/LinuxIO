package systemd

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, "systemd", []bridgeipc.Command{
		{Name: "list_timers", Mode: bridgeipc.ModeQuery, Handler: handleListTimers},
		{Name: "list_sockets", Mode: bridgeipc.ModeQuery, Handler: handleListSockets},
		{Name: "list_services", Mode: bridgeipc.ModeQuery, Handler: handleListServices},
		{Name: "get_unit_info", Mode: bridgeipc.ModeQuery, Handler: handleGetUnitInfo},
		{Name: "start_service", Mode: bridgeipc.ModeJob, Handler: handleStartService},
		{Name: "stop_service", Mode: bridgeipc.ModeJob, Handler: handleStopService},
		{Name: "restart_service", Mode: bridgeipc.ModeJob, Handler: handleRestartService},
		{Name: "reload_service", Mode: bridgeipc.ModeJob, Handler: handleReloadService},
		{Name: "enable_service", Mode: bridgeipc.ModeJob, Handler: handleEnableService},
		{Name: "disable_service", Mode: bridgeipc.ModeJob, Handler: handleDisableService},
		{Name: "mask_service", Mode: bridgeipc.ModeJob, Handler: handleMaskService},
		{Name: "unmask_service", Mode: bridgeipc.ModeJob, Handler: handleUnmaskService},
		{Name: "reset_failed_service", Mode: bridgeipc.ModeJob, Handler: handleResetFailedService},
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
