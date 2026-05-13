package terminal

import (
	"context"
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers all terminal handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	bridgeipc.RegisterRoutes(router, "terminal", []bridgeipc.Command{
		{Name: "list_shells", Mode: bridgeipc.ModeQuery, Handler: handleListShells},
	})
	router.Duplex("terminal.open", func(ctx context.Context, stream net.Conn, args []string) error {
		return HandleTerminalSession(ctx, rt, stream, args)
	})
	router.Duplex("container.open", func(ctx context.Context, stream net.Conn, args []string) error {
		return HandleContainerTerminalSession(ctx, rt, stream, args)
	})
}

func handleListShells(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if len(args) < 1 {
		return bridgeipc.EmitResult(emit, []string{}, nil)
	}
	shells, err := ListContainerShells(ctx, args[0])
	return bridgeipc.EmitResult(emit, shells, err)
}
