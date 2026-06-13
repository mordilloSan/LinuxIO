package power

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Query[apischema.NoRequest, apischema.PowerStatus]("power.get_status", apischema.Privileged()).Handle(handleGetStatus),
	apischema.Job[apischema.NoRequest, apischema.PowerStatus]("power.start", apischema.Privileged()).Handle(handleStart),
	apischema.Job[apischema.ProfileRequest, apischema.PowerStatus]("power.set_profile", apischema.Privileged()).Handle(handleSetProfile),
	apischema.Job[apischema.NoRequest, apischema.PowerStatus]("power.disable", apischema.Privileged()).Handle(handleDisable),
)

var Routes = api.Routes()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleGetStatus(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := GetStatus(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleStart(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := StartTuned(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleSetProfile(ctx context.Context, req apischema.ProfileRequest, emit bridgeipc.Events) error {
	result, err := SetProfile(ctx, req.Profile)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleDisable(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := DisableTuned(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}
