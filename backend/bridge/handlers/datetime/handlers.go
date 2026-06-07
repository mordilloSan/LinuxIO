package datetime

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Query[apischema.NoRequest, bool]("datetime.get_ntp_status").Handle(handleGetNTPStatus),
	apischema.Job[apischema.EnabledRequest, apischema.NoResponse]("datetime.set_ntp").Handle(handleSetNTP),
	apischema.Job[apischema.ISOTimeRequest, apischema.NoResponse]("datetime.set_server_time").Handle(handleSetServerTime),
	apischema.Query[apischema.NoRequest, string]("datetime.get_timezone").Handle(handleGetTimezone),
	apischema.Job[apischema.TimezoneRequest, apischema.NoResponse]("datetime.set_timezone").Handle(handleSetTimezone),
	apischema.Query[apischema.NoRequest, []string]("datetime.get_ntp_servers").Handle(handleGetNTPServers),
	apischema.Job[apischema.NTPServersRequest, apischema.NoResponse]("datetime.set_ntp_servers").Handle(handleSetNTPServers),
)

var Routes = api.Routes()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleGetNTPStatus(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := GetNTPStatus(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleSetNTP(ctx context.Context, req apischema.EnabledRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, SetNTP(ctx, req.Enabled == "true"))
}

func handleSetServerTime(ctx context.Context, req apischema.ISOTimeRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, SetServerTime(ctx, req.ISOTime))
}

func handleGetTimezone(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := GetTimezone(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleSetTimezone(ctx context.Context, req apischema.TimezoneRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, SetTimezone(ctx, req.Timezone))
}

func handleGetNTPServers(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := GetNTPServers(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleSetNTPServers(ctx context.Context, req apischema.NTPServersRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, SetNTPServers(ctx, req.Servers))
}
