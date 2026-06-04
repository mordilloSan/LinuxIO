package control

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	controlapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/control/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers host control handlers.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: controlapi.Reboot, Handle: handleReboot},
		{Route: controlapi.PowerOff, Handle: handlePowerOff},
		{Route: controlapi.Logoff, Handle: handleLogoff},
	})
}

func handleReboot(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, Reboot(ctx))
}

func handlePowerOff(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, PowerOff(ctx))
}

func handleLogoff(ctx context.Context, req apischema.SessionIDRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, Logoff(ctx, req.SessionID))
}
