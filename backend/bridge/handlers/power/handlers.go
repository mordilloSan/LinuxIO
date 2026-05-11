package power

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func RegisterHandlers(rt runtime.Runtime) {
	rpc.Register("power", rt, []rpc.Command{
		{Name: "get_status", Handler: handleGetStatus, Privileged: true},
		{Name: "start", Handler: handleStart, Privileged: true},
		{Name: "set_profile", Handler: handleSetProfile, Privileged: true},
		{Name: "disable", Handler: handleDisable, Privileged: true},
	})
}

func handleGetStatus(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetStatus(ctx)
	return rpc.EmitResult(emit, result, err)
}

func handleStart(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("TuneD start requested", "component", "power")
	result, err := StartTuned(ctx)
	return rpc.EmitResult(emit, result, err)
}

func handleSetProfile(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) != 1 {
		return ipc.ErrInvalidArgs
	}
	slog.Info("TuneD profile change requested", "component", "power", "profile", args[0])
	result, err := SetProfile(ctx, args[0])
	return rpc.EmitResult(emit, result, err)
}

func handleDisable(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("TuneD disable requested", "component", "power")
	result, err := DisableTuned(ctx)
	return rpc.EmitResult(emit, result, err)
}
