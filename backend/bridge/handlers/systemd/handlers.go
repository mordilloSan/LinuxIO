package systemd

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Query[apischema.NoRequest, []apischema.Timer]("systemd.list_timers").Handle(handleListTimers),
	apischema.Query[apischema.NoRequest, []apischema.Socket]("systemd.list_sockets").Handle(handleListSockets),
	apischema.Query[apischema.NoRequest, []apischema.Service]("systemd.list_services").Handle(handleListServices),
	apischema.Query[apischema.UnitNameRequest, apischema.UnitInfo]("systemd.get_unit_info").Handle(handleGetUnitInfo),
	apischema.Job[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.start_service").Handle(handleStartService),
	apischema.Job[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.stop_service").Handle(handleStopService),
	apischema.Job[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.restart_service").Handle(handleRestartService),
	apischema.Job[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.reload_service").Handle(handleReloadService),
	apischema.Job[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.enable_service").Handle(handleEnableService),
	apischema.Job[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.disable_service").Handle(handleDisableService),
	apischema.Job[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.mask_service").Handle(handleMaskService),
	apischema.Job[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.unmask_service").Handle(handleUnmaskService),
	apischema.Job[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.reset_failed_service").Handle(handleResetFailedService),
)

var Routes = api.Routes()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleListTimers(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := ListTimers(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleListSockets(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := ListSockets(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleListServices(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
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
