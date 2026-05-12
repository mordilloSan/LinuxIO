package hostname

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func RegisterHandlers(rt runtime.Runtime) {
	rpc.Register("dbus", rt, []rpc.Command{
		{Name: "set_hostname", Handler: handleSetHostname},
	})
}

func handleSetHostname(ctx context.Context, args []string, emit ipc.Events) error {
	name, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("set_hostname requested", "component", "dbus", "subsystem", "hostname", "service", name)
	return rpc.EmitResult(emit, nil, SetHostname(ctx, name))
}
