package systemd

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers keeps the existing dbus RPC component names while moving
// systemd command ownership into this package.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	bridgeipc.RegisterRoutes(router, "dbus", []bridgeipc.Command{
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

func handleListTimers(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListTimers(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleListSockets(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListSockets(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleListServices(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListServices(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetUnitInfo(ctx context.Context, args []string, emit bridgeipc.Events) error {
	unit, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := GetUnitInfo(ctx, unit)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleStartService(ctx context.Context, args []string, emit bridgeipc.Events) error {
	unit, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("start_service requested", "component", "systemd", "unit", unit)
	return bridgeipc.EmitResult(emit, nil, StartUnit(ctx, unit))
}

func handleStopService(ctx context.Context, args []string, emit bridgeipc.Events) error {
	unit, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("stop_service requested", "component", "systemd", "unit", unit)
	return bridgeipc.EmitResult(emit, nil, StopUnit(ctx, unit))
}

func handleRestartService(ctx context.Context, args []string, emit bridgeipc.Events) error {
	unit, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("restart_service requested", "component", "systemd", "unit", unit)
	return bridgeipc.EmitResult(emit, nil, RestartUnit(ctx, unit))
}

func handleReloadService(ctx context.Context, args []string, emit bridgeipc.Events) error {
	unit, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("reload_service requested", "component", "systemd", "unit", unit)
	return bridgeipc.EmitResult(emit, nil, ReloadUnit(ctx, unit))
}

func handleEnableService(ctx context.Context, args []string, emit bridgeipc.Events) error {
	unit, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("enable_service requested", "component", "systemd", "unit", unit)
	return bridgeipc.EmitResult(emit, nil, EnableUnit(ctx, unit))
}

func handleDisableService(ctx context.Context, args []string, emit bridgeipc.Events) error {
	unit, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("disable_service requested", "component", "systemd", "unit", unit)
	return bridgeipc.EmitResult(emit, nil, DisableUnit(ctx, unit))
}

func handleMaskService(ctx context.Context, args []string, emit bridgeipc.Events) error {
	unit, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("mask_service requested", "component", "systemd", "unit", unit)
	return bridgeipc.EmitResult(emit, nil, MaskUnit(ctx, unit))
}

func handleUnmaskService(ctx context.Context, args []string, emit bridgeipc.Events) error {
	unit, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("unmask_service requested", "component", "systemd", "unit", unit)
	return bridgeipc.EmitResult(emit, nil, UnmaskUnit(ctx, unit))
}

func handleResetFailedService(ctx context.Context, args []string, emit bridgeipc.Events) error {
	unit, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("reset_failed_service requested", "component", "systemd", "unit", unit)
	return bridgeipc.EmitResult(emit, nil, ResetFailedUnit(ctx, unit))
}
