package systemd

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers keeps the existing dbus RPC component names while moving
// systemd command ownership into this package.
func RegisterHandlers(rt runtime.Runtime) {
	rpc.Register("dbus", rt, []rpc.Command{
		{Name: "list_timers", Handler: handleListTimers},
		{Name: "list_sockets", Handler: handleListSockets},
		{Name: "list_services", Handler: handleListServices},
		{Name: "get_unit_info", Handler: handleGetUnitInfo},
		{Name: "start_service", Handler: handleStartService},
		{Name: "stop_service", Handler: handleStopService},
		{Name: "restart_service", Handler: handleRestartService},
		{Name: "reload_service", Handler: handleReloadService},
		{Name: "enable_service", Handler: handleEnableService},
		{Name: "disable_service", Handler: handleDisableService},
		{Name: "mask_service", Handler: handleMaskService},
		{Name: "unmask_service", Handler: handleUnmaskService},
		{Name: "reset_failed_service", Handler: handleResetFailedService},
	})
}

func handleListTimers(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := ListTimers(ctx)
	return rpc.EmitResult(emit, result, err)
}

func handleListSockets(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := ListSockets(ctx)
	return rpc.EmitResult(emit, result, err)
}

func handleListServices(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := ListServices(ctx)
	return rpc.EmitResult(emit, result, err)
}

func handleGetUnitInfo(ctx context.Context, args []string, emit ipc.Events) error {
	unit, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := GetUnitInfo(ctx, unit)
	return rpc.EmitResult(emit, result, err)
}

func handleStartService(ctx context.Context, args []string, emit ipc.Events) error {
	unit, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("start_service requested", "component", "systemd", "unit", unit)
	return rpc.EmitResult(emit, nil, StartUnit(ctx, unit))
}

func handleStopService(ctx context.Context, args []string, emit ipc.Events) error {
	unit, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("stop_service requested", "component", "systemd", "unit", unit)
	return rpc.EmitResult(emit, nil, StopUnit(ctx, unit))
}

func handleRestartService(ctx context.Context, args []string, emit ipc.Events) error {
	unit, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("restart_service requested", "component", "systemd", "unit", unit)
	return rpc.EmitResult(emit, nil, RestartUnit(ctx, unit))
}

func handleReloadService(ctx context.Context, args []string, emit ipc.Events) error {
	unit, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("reload_service requested", "component", "systemd", "unit", unit)
	return rpc.EmitResult(emit, nil, ReloadUnit(ctx, unit))
}

func handleEnableService(ctx context.Context, args []string, emit ipc.Events) error {
	unit, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("enable_service requested", "component", "systemd", "unit", unit)
	return rpc.EmitResult(emit, nil, EnableUnit(ctx, unit))
}

func handleDisableService(ctx context.Context, args []string, emit ipc.Events) error {
	unit, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("disable_service requested", "component", "systemd", "unit", unit)
	return rpc.EmitResult(emit, nil, DisableUnit(ctx, unit))
}

func handleMaskService(ctx context.Context, args []string, emit ipc.Events) error {
	unit, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("mask_service requested", "component", "systemd", "unit", unit)
	return rpc.EmitResult(emit, nil, MaskUnit(ctx, unit))
}

func handleUnmaskService(ctx context.Context, args []string, emit ipc.Events) error {
	unit, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("unmask_service requested", "component", "systemd", "unit", unit)
	return rpc.EmitResult(emit, nil, UnmaskUnit(ctx, unit))
}

func handleResetFailedService(ctx context.Context, args []string, emit ipc.Events) error {
	unit, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("reset_failed_service requested", "component", "systemd", "unit", unit)
	return rpc.EmitResult(emit, nil, ResetFailedUnit(ctx, unit))
}
