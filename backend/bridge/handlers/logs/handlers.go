package logs

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	logsapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/logs/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.AttachRunner(router, apischema.RunnerBinding{
		Route: logsapi.GeneralFollow,
		Runner: func(ctx context.Context, job *bridgeipc.Job, req apischema.GeneralLogsFollowRequest) (any, error) {
			return runGeneralLogsJob(ctx, rt, job, req)
		},
		Policy: bridgeipc.StreamDefault,
	})
	apischema.AttachRunner(router, apischema.RunnerBinding{
		Route: logsapi.ServiceFollow,
		Runner: func(ctx context.Context, job *bridgeipc.Job, req apischema.ServiceLogsFollowRequest) (any, error) {
			return runServiceLogsJob(ctx, rt, job, req)
		},
		Policy: bridgeipc.StreamDefault,
	})
}
