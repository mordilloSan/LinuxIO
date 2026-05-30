package terminal

import (
	"context"
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers all terminal handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, "terminal", []bridgeipc.Command{
		{Name: "list_shells", Mode: bridgeipc.ModeQuery, Handler: handleListShells},
	})
	apischema.AttachDuplex(router, apischema.DuplexBinding{
		Route: "terminal.open",
		Handle: func(ctx context.Context, stream net.Conn, req apischema.TerminalOpenRequest) error {
			return HandleTerminalSession(ctx, rt, stream, req)
		},
	})
	apischema.AttachDuplex(router, apischema.DuplexBinding{
		Route: "container.open",
		Handle: func(ctx context.Context, stream net.Conn, req apischema.ContainerOpenRequest) error {
			return HandleContainerTerminalSession(ctx, rt, stream, req)
		},
	})
}

func handleListShells(ctx context.Context, req apischema.ContainerIDRequest, emit bridgeipc.Events) error {
	if req.ContainerID == "" {
		return bridgeipc.EmitResult(emit, []string{}, nil)
	}
	shells, err := ListContainerShells(ctx, req.ContainerID)
	return bridgeipc.EmitResult(emit, shells, err)
}
