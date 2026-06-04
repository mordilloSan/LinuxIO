package logs

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routes = apischema.NewRouteCatalog()

var RouteGeneralFollow = routes.Runner("logs.general.follow", apischema.TypeOf[apischema.GeneralLogsFollowRequest](), apischema.NoResponse(), apischema.NoEndpoint())
var RouteServiceFollow = routes.Runner("logs.service.follow", apischema.TypeOf[apischema.ServiceLogsFollowRequest](), apischema.NoResponse(), apischema.NoEndpoint())

var Routes = routes.All()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.AttachRunner(router, apischema.RunnerBinding{
		Route: RouteGeneralFollow,
		Runner: func(ctx context.Context, job *bridgeipc.Job, req apischema.GeneralLogsFollowRequest) (any, error) {
			return runGeneralLogsJob(ctx, rt, job, req)
		},
		Policy: bridgeipc.StreamDefault,
	})
	apischema.AttachRunner(router, apischema.RunnerBinding{
		Route: RouteServiceFollow,
		Runner: func(ctx context.Context, job *bridgeipc.Job, req apischema.ServiceLogsFollowRequest) (any, error) {
			return runServiceLogsJob(ctx, rt, job, req)
		},
		Policy: bridgeipc.StreamDefault,
	})
}
