package appupdate

import (
	"context"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	appupdateapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/appupdate/api"
	controlapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/control/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers control handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: controlapi.Version, Handle: handleVersion},
	})
	policy := bridgeipc.SingletonSystem
	policy.Timeout = 30 * time.Minute
	apischema.AttachRunner(router, apischema.RunnerBinding{
		Route: appupdateapi.ControlAppUpdate,
		Runner: func(ctx context.Context, job *bridgeipc.Job, req apischema.AppUpdateRequest) (any, error) {
			return runAppUpdateJob(ctx, rt, job, req)
		},
		Policy: policy,
	})
}

func handleVersion(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	info, err := getVersionInfo(ctx)
	return bridgeipc.EmitResult(emit, info, err)
}
