package control

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Job[apischema.NoRequest, apischema.NoResponse]("control.reboot").HandleVoid(handleReboot),
	apischema.Job[apischema.NoRequest, apischema.NoResponse]("control.power_off").HandleVoid(handlePowerOff),
	apischema.Job[apischema.SessionIDRequest, apischema.NoResponse]("control.logoff").HandleVoid(handleLogoff),
)

var Routes = api.Routes()

// RegisterHandlers registers host control handlers.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleReboot(ctx context.Context, _ apischema.NoRequest) error {
	return Reboot(ctx)
}

func handlePowerOff(ctx context.Context, _ apischema.NoRequest) error {
	return PowerOff(ctx)
}

func handleLogoff(ctx context.Context, req apischema.SessionIDRequest) error {
	return Logoff(ctx, req.SessionID)
}
