package power

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	powerapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/power/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: powerapi.GetStatus, Handle: handleGetStatus},
		{Route: powerapi.Start, Handle: handleStart},
		{Route: powerapi.SetProfile, Handle: handleSetProfile},
		{Route: powerapi.Disable, Handle: handleDisable},
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
