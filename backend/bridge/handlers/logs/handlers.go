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
		apischema.Runner[apischema.GeneralLogsFollowRequest, apischema.NoResponse](streamTypeGeneralLogs, apischema.NoEndpoint()).Run(
			func(ctx context.Context, job *bridgeipc.Job, req apischema.GeneralLogsFollowRequest) (any, error) {
				return runGeneralLogsJob(ctx, rt, job, req)
			},
			bridgeipc.StreamFollow,
		),
		apischema.Runner[apischema.ServiceLogsFollowRequest, apischema.NoResponse](streamTypeServiceLogs, apischema.NoEndpoint()).Run(
			func(ctx context.Context, job *bridgeipc.Job, req apischema.ServiceLogsFollowRequest) (any, error) {
				return runServiceLogsJob(ctx, rt, job, req)
			},
			bridgeipc.StreamFollow,
		),
		apischema.Query[apischema.GeneralLogEntryRequest, map[string]any]("logs.general_entry").Handle(handleGeneralLogEntry),
		apischema.Query[apischema.GeneralLogsPageRequest, apischema.GeneralLogsPageResponse]("logs.general_page").Handle(handleGeneralLogsPage),
	)
}

func handleGeneralLogEntry(ctx context.Context, req apischema.GeneralLogEntryRequest) (map[string]any, error) {
	return GetGeneralLogEntry(ctx, req.Cursor)
}

func handleGeneralLogsPage(ctx context.Context, req apischema.GeneralLogsPageRequest) (apischema.GeneralLogsPageResponse, error) {
	return GetGeneralLogsPage(ctx, req)
}

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	routeBindings(rt).Register(router)
}
