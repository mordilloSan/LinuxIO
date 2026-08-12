package systemd

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Call[apischema.NoRequest, []apischema.Timer]("systemd.list_timers", apischema.RetrySafe()).Handle(handleListTimers),
	apischema.Call[apischema.NoRequest, []apischema.Socket]("systemd.list_sockets", apischema.RetrySafe()).Handle(handleListSockets),
	apischema.Call[apischema.NoRequest, []apischema.Service]("systemd.list_services", apischema.RetrySafe()).Handle(handleListServices),
	apischema.Call[apischema.UnitNameRequest, apischema.UnitInfo]("systemd.get_unit_info", apischema.RetrySafe()).Handle(handleGetUnitInfo),
	apischema.Call[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.start_service").HandleVoid(handleStartService),
	apischema.Call[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.stop_service").HandleVoid(handleStopService),
	apischema.Call[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.restart_service").HandleVoid(handleRestartService),
	apischema.Call[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.reload_service").HandleVoid(handleReloadService),
	apischema.Call[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.enable_service").HandleVoid(handleEnableService),
	apischema.Call[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.disable_service").HandleVoid(handleDisableService),
	apischema.Call[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.mask_service").HandleVoid(handleMaskService),
	apischema.Call[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.unmask_service").HandleVoid(handleUnmaskService),
	apischema.Call[apischema.ServiceNameRequest, apischema.NoResponse]("systemd.reset_failed_service").HandleVoid(handleResetFailedService),
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

func handleGetUnitInfo(ctx context.Context, req apischema.UnitNameRequest) (apischema.UnitInfo, error) {
	return GetUnitInfo(ctx, req.UnitName)
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
