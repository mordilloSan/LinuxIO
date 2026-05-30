package power

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, "power", []bridgeipc.Command{
		{Name: "get_status", Mode: bridgeipc.ModeQuery, Handler: handleGetStatus, Privileged: true},
		{Name: "start", Mode: bridgeipc.ModeJob, Handler: handleStart, Privileged: true},
		{Name: "set_profile", Mode: bridgeipc.ModeJob, Handler: handleSetProfile, Privileged: true},
		{Name: "disable", Mode: bridgeipc.ModeJob, Handler: handleDisable, Privileged: true},
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
