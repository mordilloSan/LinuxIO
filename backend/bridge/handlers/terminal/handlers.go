package terminal

import (
	"context"
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var Routes = routeBindings(runtime.Runtime{}).Routes()

func routeBindings(rt runtime.Runtime) apischema.BindingSet {
	return apischema.Bindings(
		apischema.Query[apischema.ContainerIDRequest, []string]("terminal.list_shells").Handle(handleListShells),
		apischema.DuplexRoute[apischema.TerminalOpenRequest, apischema.NoResponse]("terminal.open", apischema.NoEndpoint()).Duplex(
			func(ctx context.Context, stream net.Conn, req apischema.TerminalOpenRequest) error {
				return HandleTerminalSession(ctx, rt, stream, req)
			},
		),
		apischema.DuplexRoute[apischema.ContainerOpenRequest, apischema.NoResponse]("container.open", apischema.NoEndpoint()).Duplex(
			func(ctx context.Context, stream net.Conn, req apischema.ContainerOpenRequest) error {
				return HandleContainerTerminalSession(ctx, rt, stream, req)
			},
		),
	)
}

// RegisterHandlers registers all terminal handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	routeBindings(rt).Register(router)
}

func handleListShells(ctx context.Context, req apischema.ContainerIDRequest, emit bridgeipc.Events) error {
	if req.ContainerID == "" {
		return bridgeipc.EmitResult(emit, []string{}, nil)
	}
	shells, err := ListContainerShells(ctx, req.ContainerID)
	return bridgeipc.EmitResult(emit, shells, err)
}
