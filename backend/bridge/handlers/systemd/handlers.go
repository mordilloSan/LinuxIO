package systemd

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routes = apischema.NewRouteCatalog()

var RouteDisableService = routes.Job("systemd.disable_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())
var RouteEnableService = routes.Job("systemd.enable_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())
var RouteGetUnitInfo = routes.Query("systemd.get_unit_info", apischema.TypeOf[apischema.UnitNameRequest](), apischema.TypeOf[apischema.UnitInfo]())
var RouteListServices = routes.Query("systemd.list_services", apischema.NoRequest(), apischema.TypeOf[[]apischema.Service]())
var RouteListSockets = routes.Query("systemd.list_sockets", apischema.NoRequest(), apischema.TypeOf[[]apischema.Socket]())
var RouteListTimers = routes.Query("systemd.list_timers", apischema.NoRequest(), apischema.TypeOf[[]apischema.Timer]())
var RouteMaskService = routes.Job("systemd.mask_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())
var RouteReloadService = routes.Job("systemd.reload_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())
var RouteResetFailedService = routes.Job("systemd.reset_failed_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())
var RouteRestartService = routes.Job("systemd.restart_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())
var RouteStartService = routes.Job("systemd.start_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())
var RouteStopService = routes.Job("systemd.stop_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())
var RouteUnmaskService = routes.Job("systemd.unmask_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())

var Routes = routes.All()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router,
		RouteListTimers.Handle(handleListTimers),
		RouteListSockets.Handle(handleListSockets),
		RouteListServices.Handle(handleListServices),
		RouteGetUnitInfo.Handle(handleGetUnitInfo),
		RouteStartService.Handle(handleStartService),
		RouteStopService.Handle(handleStopService),
		RouteRestartService.Handle(handleRestartService),
		RouteReloadService.Handle(handleReloadService),
		RouteEnableService.Handle(handleEnableService),
		RouteDisableService.Handle(handleDisableService),
		RouteMaskService.Handle(handleMaskService),
		RouteUnmaskService.Handle(handleUnmaskService),
		RouteResetFailedService.Handle(handleResetFailedService),
	)
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
