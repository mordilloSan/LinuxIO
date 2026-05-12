package control

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers host control handlers.
func RegisterHandlers(rt runtime.Runtime) {
	rpc.Register("dbus", rt, []rpc.Command{
		{Name: "reboot", Handler: handleReboot},
		{Name: "power_off", Handler: handlePowerOff},
		{Name: "logoff", Handler: handleLogoff},
	})
}

func handleReboot(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("reboot requested", "component", "dbus", "subsystem", "login1")
	return rpc.EmitResult(emit, nil, Reboot(ctx))
}

func handlePowerOff(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("power_off requested", "component", "dbus", "subsystem", "login1")
	return rpc.EmitResult(emit, nil, PowerOff(ctx))
}

func handleLogoff(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 1); err != nil {
		return err
	}
	sessionID := args[0]
	slog.Info("logoff requested", "component", "dbus", "subsystem", "login1", "sessionID", sessionID)
	return rpc.EmitResult(emit, nil, Logoff(ctx, sessionID))
}
