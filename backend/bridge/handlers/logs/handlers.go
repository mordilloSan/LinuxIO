package logs

import (
	"context"
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var Routes = routeBindings(runtime.Runtime{}).Routes()

func routeBindings(rt runtime.Runtime) apischema.BindingSet {
	return apischema.Bindings(
		apischema.DuplexRoute[apischema.GeneralLogsFollowRequest, apischema.NoResponse](streamTypeGeneralLogs, apischema.NoEndpoint()).Duplex(
			func(ctx context.Context, stream net.Conn, req apischema.GeneralLogsFollowRequest) error {
				return streamGeneralLogsChannel(ctx, stream, rt, req)
			},
		),
		apischema.DuplexRoute[apischema.ServiceLogsFollowRequest, apischema.NoResponse](streamTypeServiceLogs, apischema.NoEndpoint()).Duplex(
			func(ctx context.Context, stream net.Conn, req apischema.ServiceLogsFollowRequest) error {
				return streamServiceLogsChannel(ctx, stream, rt, req)
			},
		),
		apischema.Call[apischema.GeneralLogEntryRequest, map[string]any]("logs.general_entry", apischema.RetrySafe()).Handle(handleGeneralLogEntry),
		apischema.Call[apischema.GeneralLogsPageRequest, apischema.GeneralLogsPageResponse]("logs.general_page", apischema.RetrySafe()).Handle(handleGeneralLogsPage),
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
