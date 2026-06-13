package control

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Job[apischema.NoRequest, apischema.NoResponse]("control.reboot").Handle(handleReboot),
	apischema.Job[apischema.NoRequest, apischema.NoResponse]("control.power_off").Handle(handlePowerOff),
	apischema.Job[apischema.SessionIDRequest, apischema.NoResponse]("control.logoff").Handle(handleLogoff),
)

var Routes = api.Routes()

// RegisterHandlers registers host control handlers.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleReboot(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, Reboot(ctx))
}

func handlePowerOff(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, PowerOff(ctx))
}

func handleLogoff(ctx context.Context, req apischema.SessionIDRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, Logoff(ctx, req.SessionID))
}
