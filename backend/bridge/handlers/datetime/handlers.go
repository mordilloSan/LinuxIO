package datetime

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func RegisterHandlers(rt runtime.Runtime) {
	rpc.Register("dbus", rt, []rpc.Command{
		{Name: "get_ntp_status", Handler: handleGetNTPStatus},
		{Name: "set_ntp", Handler: handleSetNTP},
		{Name: "set_server_time", Handler: handleSetServerTime},
		{Name: "get_timezone", Handler: handleGetTimezone},
		{Name: "set_timezone", Handler: handleSetTimezone},
		{Name: "get_ntp_servers", Handler: handleGetNTPServers},
		{Name: "set_ntp_servers", Handler: handleSetNTPServers},
	})
}

func handleGetNTPStatus(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetNTPStatus(ctx)
	return rpc.EmitResult(emit, result, err)
}

func handleSetNTP(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) != 1 {
		return ipc.ErrInvalidArgs
	}
	enabled := args[0] == "true"
	slog.Info("set_ntp requested", "component", "dbus", "subsystem", "timedate", "enabled", enabled)
	return rpc.EmitResult(emit, nil, SetNTP(ctx, enabled))
}

func handleSetServerTime(ctx context.Context, args []string, emit ipc.Events) error {
	mode, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("set_server_time requested", "component", "dbus", "subsystem", "timedate", "mode", mode)
	return rpc.EmitResult(emit, nil, SetServerTime(ctx, mode))
}

func handleGetTimezone(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetTimezone(ctx)
	return rpc.EmitResult(emit, result, err)
}

func handleSetTimezone(ctx context.Context, args []string, emit ipc.Events) error {
	timezone, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("set_timezone requested", "component", "dbus", "subsystem", "timedate", "mode", timezone)
	return rpc.EmitResult(emit, nil, SetTimezone(ctx, timezone))
}

func handleGetNTPServers(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetNTPServers(ctx)
	return rpc.EmitResult(emit, result, err)
}

func handleSetNTPServers(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("set_ntp_servers requested", "component", "dbus", "subsystem", "timedate", "server_count", len(args))
	return rpc.EmitResult(emit, nil, SetNTPServers(ctx, args))
}
