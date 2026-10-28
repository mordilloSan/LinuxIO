package datetime

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
)

var api = apischema.Bindings(
	apischema.Call[apischema.NoRequest, bool]("datetime.get_ntp_status", apischema.RetrySafe()).Handle(handleGetNTPStatus),
	apischema.MutationVoid("datetime.set_ntp", handleSetNTP),
	apischema.MutationVoid("datetime.set_server_time", handleSetServerTime),
	apischema.Call[apischema.NoRequest, string]("datetime.get_timezone", apischema.RetrySafe()).Handle(handleGetTimezone),
	apischema.MutationVoid("datetime.set_timezone", handleSetTimezone),
	apischema.Call[apischema.NoRequest, []string]("datetime.get_ntp_servers", apischema.RetrySafe()).Handle(handleGetNTPServers),
	apischema.MutationVoid("datetime.set_ntp_servers", handleSetNTPServers),
)

var Routes = api.Routes()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleGetNTPStatus(ctx context.Context, _ apischema.NoRequest) (bool, error) {
	return GetNTPStatus(ctx)
}

func handleSetNTP(ctx context.Context, req apischema.EnabledRequest) error {
	enabled, err := strconv.ParseBool(req.Enabled)
	if err != nil {
		return fmt.Errorf("%w: enabled: %w", bridgeipc.ErrInvalidArgs, err)
	}
	return SetNTP(ctx, enabled)
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
