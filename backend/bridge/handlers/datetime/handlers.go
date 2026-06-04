package datetime

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	datetimeapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/datetime/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: datetimeapi.GetNTPStatus, Handle: handleGetNTPStatus},
		{Route: datetimeapi.SetNTP, Handle: handleSetNTP},
		{Route: datetimeapi.SetServerTime, Handle: handleSetServerTime},
		{Route: datetimeapi.GetTimezone, Handle: handleGetTimezone},
		{Route: datetimeapi.SetTimezone, Handle: handleSetTimezone},
		{Route: datetimeapi.GetNTPServers, Handle: handleGetNTPServers},
		{Route: datetimeapi.SetNTPServers, Handle: handleSetNTPServers},
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
