package systemd

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

var api = apischema.Bindings(
	apischema.Call[apischema.NoRequest, []apischema.Timer]("systemd.list_timers", apischema.RetrySafe()).Handle(handleListTimers),
	apischema.Call[apischema.NoRequest, []apischema.Socket]("systemd.list_sockets", apischema.RetrySafe()).Handle(handleListSockets),
	apischema.Call[apischema.NoRequest, []apischema.Service]("systemd.list_services", apischema.RetrySafe()).Handle(handleListServices),
	apischema.Call[apischema.UnitNameRequest, apischema.UnitInfo]("systemd.get_unit_info", apischema.RetrySafe()).Handle(handleGetUnitInfo),
	apischema.MutationVoid("systemd.start_service", handleStartService),
	apischema.MutationVoid("systemd.stop_service", handleStopService),
	apischema.MutationVoid("systemd.restart_service", handleRestartService),
	apischema.MutationVoid("systemd.reload_service", handleReloadService),
	apischema.MutationVoid("systemd.enable_service", handleEnableService),
	apischema.MutationVoid("systemd.disable_service", handleDisableService),
	apischema.MutationVoid("systemd.mask_service", handleMaskService),
	apischema.MutationVoid("systemd.unmask_service", handleUnmaskService),
	apischema.MutationVoid("systemd.reset_failed_service", handleResetFailedService),
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
	return systemd.StartUnit(ctx, req.ServiceName)
}

func handleStopService(ctx context.Context, req apischema.ServiceNameRequest) error {
	return systemd.StopUnit(ctx, req.ServiceName)
}

func handleRestartService(ctx context.Context, req apischema.ServiceNameRequest) error {
	return systemd.RestartUnit(ctx, req.ServiceName)
}

func handleReloadService(ctx context.Context, req apischema.ServiceNameRequest) error {
	return systemd.ReloadUnit(ctx, req.ServiceName)
}

func handleEnableService(ctx context.Context, req apischema.ServiceNameRequest) error {
	return systemd.EnableUnit(ctx, req.ServiceName)
}

func handleDisableService(ctx context.Context, req apischema.ServiceNameRequest) error {
	return systemd.DisableUnit(ctx, req.ServiceName)
}

func handleMaskService(ctx context.Context, req apischema.ServiceNameRequest) error {
	return systemd.MaskUnit(ctx, req.ServiceName)
}

func handleUnmaskService(ctx context.Context, req apischema.ServiceNameRequest) error {
	return systemd.UnmaskUnit(ctx, req.ServiceName)
}

func handleResetFailedService(ctx context.Context, req apischema.ServiceNameRequest) error {
	return systemd.ResetFailedUnit(ctx, req.ServiceName)
}
