package logs

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	router.JobRunner("logs.general.follow", func(ctx context.Context, job *bridgeipc.Job, args []string) (any, error) {
		return runGeneralLogsJob(ctx, rt, job, args)
	}, bridgeipc.StreamDefault)
	router.JobRunner("logs.service.follow", func(ctx context.Context, job *bridgeipc.Job, args []string) (any, error) {
		return runServiceLogsJob(ctx, rt, job, args)
	}, bridgeipc.StreamDefault)
}
