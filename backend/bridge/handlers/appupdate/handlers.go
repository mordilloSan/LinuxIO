package appupdate

import (
	"context"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers control handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, "control", []bridgeipc.Command{
		{Name: "version", Mode: bridgeipc.ModeQuery, Handler: handleVersion},
	})
	policy := bridgeipc.SingletonSystem
	policy.Timeout = 30 * time.Minute
	apischema.AttachRunner(router, apischema.RunnerBinding{
		Route: "control.app_update",
		Runner: func(ctx context.Context, job *bridgeipc.Job, args []string) (any, error) {
			return runAppUpdateJob(ctx, rt, job, args)
		},
		Policy: policy,
	})
}

func handleVersion(ctx context.Context, args []string, emit bridgeipc.Events) error {
	info, err := getVersionInfo(ctx)
	return bridgeipc.EmitResult(emit, info, err)
}
