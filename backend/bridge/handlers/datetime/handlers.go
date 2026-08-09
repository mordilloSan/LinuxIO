package datetime

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Call[apischema.NoRequest, bool]("datetime.get_ntp_status").Handle(handleGetNTPStatus),
	apischema.Call[apischema.EnabledRequest, apischema.NoResponse]("datetime.set_ntp").HandleVoid(handleSetNTP),
	apischema.Call[apischema.ISOTimeRequest, apischema.NoResponse]("datetime.set_server_time").HandleVoid(handleSetServerTime),
	apischema.Call[apischema.NoRequest, string]("datetime.get_timezone").Handle(handleGetTimezone),
	apischema.Call[apischema.TimezoneRequest, apischema.NoResponse]("datetime.set_timezone").HandleVoid(handleSetTimezone),
	apischema.Call[apischema.NoRequest, []string]("datetime.get_ntp_servers").Handle(handleGetNTPServers),
	apischema.Call[apischema.NTPServersRequest, apischema.NoResponse]("datetime.set_ntp_servers").HandleVoid(handleSetNTPServers),
)

var Routes = api.Routes()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleGetNTPStatus(ctx context.Context, _ apischema.NoRequest) (bool, error) {
	return GetNTPStatus(ctx)
}

func handleSetNTP(ctx context.Context, req apischema.EnabledRequest) error {
	return SetNTP(ctx, req.Enabled == "true")
}

func handleSetServerTime(ctx context.Context, req apischema.ISOTimeRequest) error {
	return SetServerTime(ctx, req.ISOTime)
}

func handleGetTimezone(ctx context.Context, _ apischema.NoRequest) (string, error) {
	return GetTimezone(ctx)
}

func handleSetTimezone(ctx context.Context, req apischema.TimezoneRequest) error {
	return SetTimezone(ctx, req.Timezone)
}

func handleGetNTPServers(ctx context.Context, _ apischema.NoRequest) ([]string, error) {
	return GetNTPServers(ctx)
}

func handleSetNTPServers(ctx context.Context, req apischema.NTPServersRequest) error {
	return SetNTPServers(ctx, req.Servers)
}
