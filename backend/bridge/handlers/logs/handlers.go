package logs

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.AttachRunner(router, apischema.RunnerBinding{
		Route: "logs.general.follow",
		Runner: func(ctx context.Context, job *bridgeipc.Job, req apischema.GeneralLogsFollowRequest) (any, error) {
			return runGeneralLogsJob(ctx, rt, job, req)
		},
		Policy: bridgeipc.StreamDefault,
	})
	apischema.AttachRunner(router, apischema.RunnerBinding{
		Route: "logs.service.follow",
		Runner: func(ctx context.Context, job *bridgeipc.Job, req apischema.ServiceLogsFollowRequest) (any, error) {
			return runServiceLogsJob(ctx, rt, job, req)
		},
		Policy: bridgeipc.StreamDefault,
	})
}
