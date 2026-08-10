package power

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Call[apischema.NoRequest, apischema.PowerStatus]("power.get_status", apischema.RetrySafe(), apischema.Privileged()).Handle(handleGetStatus),
	apischema.Call[apischema.NoRequest, apischema.PowerStatus]("power.start", apischema.Privileged()).Handle(handleStart),
	apischema.Call[apischema.ProfileRequest, apischema.PowerStatus]("power.set_profile", apischema.Privileged()).Handle(handleSetProfile),
	apischema.Call[apischema.NoRequest, apischema.PowerStatus]("power.disable", apischema.Privileged()).Handle(handleDisable),
)

var Routes = api.Routes()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleGetStatus(ctx context.Context, _ apischema.NoRequest) (apischema.PowerStatus, error) {
	return GetStatus(ctx)
}

func handleStart(ctx context.Context, _ apischema.NoRequest) (apischema.PowerStatus, error) {
	return StartTuned(ctx)
}

func handleSetProfile(ctx context.Context, req apischema.ProfileRequest) (apischema.PowerStatus, error) {
	return SetProfile(ctx, req.Profile)
}

func handleDisable(ctx context.Context, _ apischema.NoRequest) (apischema.PowerStatus, error) {
	return DisableTuned(ctx)
}
