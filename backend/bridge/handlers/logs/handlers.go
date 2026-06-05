package logs

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var Routes = routeBindings(runtime.Runtime{}).Routes()

func routeBindings(rt runtime.Runtime) apischema.BindingSet {
	return apischema.Bindings(
		apischema.Runner(streamTypeGeneralLogs, apischema.TypeOf[apischema.GeneralLogsFollowRequest](), apischema.NoResponse(), apischema.NoEndpoint()).Run(
			func(ctx context.Context, job *bridgeipc.Job, req apischema.GeneralLogsFollowRequest) (any, error) {
				return runGeneralLogsJob(ctx, rt, job, req)
			},
			bridgeipc.StreamDefault,
		),
		apischema.Runner(streamTypeServiceLogs, apischema.TypeOf[apischema.ServiceLogsFollowRequest](), apischema.NoResponse(), apischema.NoEndpoint()).Run(
			func(ctx context.Context, job *bridgeipc.Job, req apischema.ServiceLogsFollowRequest) (any, error) {
				return runServiceLogsJob(ctx, rt, job, req)
			},
			bridgeipc.StreamDefault,
		),
	)
}

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	routeBindings(rt).Register(router)
}
