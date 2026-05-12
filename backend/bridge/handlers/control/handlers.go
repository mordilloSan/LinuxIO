package control

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers host control handlers.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	bridgeipc.RegisterRoutes(router, "dbus", []bridgeipc.Command{
		{Name: "reboot", Mode: bridgeipc.ModeJob, Handler: handleReboot},
		{Name: "power_off", Mode: bridgeipc.ModeJob, Handler: handlePowerOff},
		{Name: "logoff", Mode: bridgeipc.ModeJob, Handler: handleLogoff},
	})
}

func handleReboot(ctx context.Context, args []string, emit bridgeipc.Events) error {
	slog.Info("reboot requested", "component", "dbus", "subsystem", "login1")
	return bridgeipc.EmitResult(emit, nil, Reboot(ctx))
}

func handlePowerOff(ctx context.Context, args []string, emit bridgeipc.Events) error {
	slog.Info("power_off requested", "component", "dbus", "subsystem", "login1")
	return bridgeipc.EmitResult(emit, nil, PowerOff(ctx))
}

func handleLogoff(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 1); err != nil {
		return err
	}
	sessionID := args[0]
	slog.Info("logoff requested", "component", "dbus", "subsystem", "login1", "sessionID", sessionID)
	return bridgeipc.EmitResult(emit, nil, Logoff(ctx, sessionID))
}
