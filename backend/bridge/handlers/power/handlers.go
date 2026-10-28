package power

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
)

var api = apischema.Bindings(
	apischema.Call[apischema.NoRequest, apischema.PowerStatus]("power.get_status", apischema.RetrySafe(), apischema.Privileged()).Handle(handleGetStatus),
	apischema.Mutation("power.start", handleStart, apischema.Privileged()),
	apischema.Mutation("power.set_profile", handleSetProfile, apischema.Privileged()),
	apischema.Mutation("power.disable", handleDisable, apischema.Privileged()),
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
