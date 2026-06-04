package datetime

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routes = apischema.NewRouteCatalog()

var RouteGetNTPServers = routes.Query("datetime.get_ntp_servers", apischema.NoRequest(), apischema.TypeOf[[]string]())
var RouteGetNTPStatus = routes.Query("datetime.get_ntp_status", apischema.NoRequest(), apischema.TypeOf[bool]())
var RouteGetTimezone = routes.Query("datetime.get_timezone", apischema.NoRequest(), apischema.TypeOf[string]())
var RouteSetNTP = routes.Job("datetime.set_ntp", apischema.TypeOf[apischema.EnabledRequest](), apischema.NoResponse())
var RouteSetNTPServers = routes.Job("datetime.set_ntp_servers", apischema.TypeOf[apischema.NTPServersRequest](), apischema.NoResponse())
var RouteSetServerTime = routes.Job("datetime.set_server_time", apischema.TypeOf[apischema.ISOTimeRequest](), apischema.NoResponse())
var RouteSetTimezone = routes.Job("datetime.set_timezone", apischema.TypeOf[apischema.TimezoneRequest](), apischema.NoResponse())

var Routes = routes.All()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: RouteGetNTPStatus, Handle: handleGetNTPStatus},
		{Route: RouteSetNTP, Handle: handleSetNTP},
		{Route: RouteSetServerTime, Handle: handleSetServerTime},
		{Route: RouteGetTimezone, Handle: handleGetTimezone},
		{Route: RouteSetTimezone, Handle: handleSetTimezone},
		{Route: RouteGetNTPServers, Handle: handleGetNTPServers},
		{Route: RouteSetNTPServers, Handle: handleSetNTPServers},
	})
}

func handleGetNTPStatus(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := GetNTPStatus(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleSetNTP(ctx context.Context, req apischema.EnabledRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, SetNTP(ctx, req.Enabled == "true"))
}

func handleSetServerTime(ctx context.Context, req apischema.ISOTimeRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, SetServerTime(ctx, req.ISOTime))
}

func handleGetTimezone(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := GetTimezone(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleSetTimezone(ctx context.Context, req apischema.TimezoneRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, SetTimezone(ctx, req.Timezone))
}

func handleGetNTPServers(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := GetNTPServers(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleSetNTPServers(ctx context.Context, req apischema.NTPServersRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, SetNTPServers(ctx, req.Servers))
}
