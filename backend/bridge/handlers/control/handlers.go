package control

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routes = apischema.NewRouteCatalog()

var RouteLogoff = routes.Job("control.logoff", apischema.TypeOf[apischema.SessionIDRequest](), apischema.NoResponse())
var RoutePowerOff = routes.Job("control.power_off", apischema.NoRequest(), apischema.NoResponse())
var RouteReboot = routes.Job("control.reboot", apischema.NoRequest(), apischema.NoResponse())
var RouteVersion = routes.Query("control.version", apischema.NoRequest(), apischema.TypeOf[apischema.VersionResponse]())

var Routes = routes.All()

// RegisterHandlers registers host control handlers.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router,
		RouteReboot.Handle(handleReboot),
		RoutePowerOff.Handle(handlePowerOff),
		RouteLogoff.Handle(handleLogoff),
	)
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
