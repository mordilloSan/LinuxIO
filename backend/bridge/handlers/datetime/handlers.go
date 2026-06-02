package datetime

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, "datetime", []bridgeipc.Command{
		{Name: "get_ntp_status", Mode: bridgeipc.ModeQuery, Handler: handleGetNTPStatus},
		{Name: "set_ntp", Mode: bridgeipc.ModeJob, Handler: handleSetNTP},
		{Name: "set_server_time", Mode: bridgeipc.ModeJob, Handler: handleSetServerTime},
		{Name: "get_timezone", Mode: bridgeipc.ModeQuery, Handler: handleGetTimezone},
		{Name: "set_timezone", Mode: bridgeipc.ModeJob, Handler: handleSetTimezone},
		{Name: "get_ntp_servers", Mode: bridgeipc.ModeQuery, Handler: handleGetNTPServers},
		{Name: "set_ntp_servers", Mode: bridgeipc.ModeJob, Handler: handleSetNTPServers},
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
