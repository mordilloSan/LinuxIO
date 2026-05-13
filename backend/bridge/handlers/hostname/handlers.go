package hostname

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	bridgeipc.RegisterRoutes(router, "dbus", []bridgeipc.Command{
		{Name: "set_hostname", Mode: bridgeipc.ModeJob, Handler: handleSetHostname},
	})
}

func handleSetHostname(ctx context.Context, args []string, emit bridgeipc.Events) error {
	name, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("set_hostname requested", "component", "dbus", "subsystem", "hostname", "service", name)
	return bridgeipc.EmitResult(emit, nil, SetHostname(ctx, name))
}
