package hostname

import (
	"context"

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
	return bridgeipc.EmitResult(emit, nil, SetHostname(ctx, name))
}
