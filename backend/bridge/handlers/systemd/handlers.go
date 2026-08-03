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
	apischema.Query[apischema.UnitNameRequest, apischema.UnitInfo]("systemd.get_unit_info").HandleEvents(handleGetUnitInfo),
	apischema.Query[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.start_service").HandleVoid(handleStartService),
	apischema.Query[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.stop_service").HandleVoid(handleStopService),
	apischema.Query[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.restart_service").HandleVoid(handleRestartService),
	apischema.Query[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.reload_service").HandleVoid(handleReloadService),
	apischema.Query[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.enable_service").HandleVoid(handleEnableService),
	apischema.Query[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.disable_service").HandleVoid(handleDisableService),
	apischema.Query[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.mask_service").HandleVoid(handleMaskService),
	apischema.Query[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.unmask_service").HandleVoid(handleUnmaskService),
	apischema.Query[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.reset_failed_service").HandleVoid(handleResetFailedService),
)

var Routes = api.Routes()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleListTimers(ctx context.Context, _ apischema.NoRequest) ([]apischema.Timer, error) {
	result, err := ListTimers(ctx)
	return timersToAPI(result), err
}

func handleListSockets(ctx context.Context, _ apischema.NoRequest) ([]apischema.Socket, error) {
	result, err := ListSockets(ctx)
	return socketsToAPI(result), err
}

func handleListServices(ctx context.Context, _ apischema.NoRequest) ([]apischema.Service, error) {
	result, err := ListServices(ctx)
	return servicesToAPI(result), err
}

func handleGetUnitInfo(ctx context.Context, req apischema.UnitNameRequest, emit bridgeipc.Events) error {
	result, err := GetUnitInfo(ctx, req.UnitName)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleStartService(ctx context.Context, req apischema.ServiceNameRequest) error {
	return StartUnit(ctx, req.ServiceName)
}

func handleStopService(ctx context.Context, req apischema.ServiceNameRequest) error {
	return StopUnit(ctx, req.ServiceName)
}

func handleRestartService(ctx context.Context, req apischema.ServiceNameRequest) error {
	return RestartUnit(ctx, req.ServiceName)
}

func handleReloadService(ctx context.Context, req apischema.ServiceNameRequest) error {
	return ReloadUnit(ctx, req.ServiceName)
}

func handleEnableService(ctx context.Context, req apischema.ServiceNameRequest) error {
	return EnableUnit(ctx, req.ServiceName)
}

func handleDisableService(ctx context.Context, req apischema.ServiceNameRequest) error {
	return DisableUnit(ctx, req.ServiceName)
}

func handleMaskService(ctx context.Context, req apischema.ServiceNameRequest) error {
	return MaskUnit(ctx, req.ServiceName)
}

func handleUnmaskService(ctx context.Context, req apischema.ServiceNameRequest) error {
	return UnmaskUnit(ctx, req.ServiceName)
}

func handleResetFailedService(ctx context.Context, req apischema.ServiceNameRequest) error {
	return ResetFailedUnit(ctx, req.ServiceName)
}
