package power

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routes = apischema.NewRouteCatalog()

var RouteDisable = routes.Job("power.disable", apischema.NoRequest(), apischema.TypeOf[apischema.PowerStatus](), apischema.Privileged())
var RouteGetStatus = routes.Query("power.get_status", apischema.NoRequest(), apischema.TypeOf[apischema.PowerStatus](), apischema.Privileged())
var RouteSetProfile = routes.Job("power.set_profile", apischema.TypeOf[apischema.ProfileRequest](), apischema.TypeOf[apischema.PowerStatus](), apischema.Privileged())
var RouteStart = routes.Job("power.start", apischema.NoRequest(), apischema.TypeOf[apischema.PowerStatus](), apischema.Privileged())

var Routes = routes.All()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: RouteGetStatus, Handle: handleGetStatus},
		{Route: RouteStart, Handle: handleStart},
		{Route: RouteSetProfile, Handle: handleSetProfile},
		{Route: RouteDisable, Handle: handleDisable},
	})
}

func handleGetStatus(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := GetStatus(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleStart(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := StartTuned(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleSetProfile(ctx context.Context, req apischema.ProfileRequest, emit bridgeipc.Events) error {
	result, err := SetProfile(ctx, req.Profile)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleDisable(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := DisableTuned(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}
