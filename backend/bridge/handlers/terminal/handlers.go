package terminal

import (
	"context"
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routes = apischema.NewRouteCatalog()

var RouteContainerOpen = routes.Duplex("container.open", apischema.TypeOf[apischema.ContainerOpenRequest](), apischema.NoResponse(), apischema.NoEndpoint())
var RouteListShells = routes.Query("terminal.list_shells", apischema.TypeOf[apischema.ContainerIDRequest](), apischema.TypeOf[[]string]())
var RouteOpen = routes.Duplex("terminal.open", apischema.TypeOf[apischema.TerminalOpenRequest](), apischema.NoResponse(), apischema.NoEndpoint())

var Routes = routes.All()

// RegisterHandlers registers all terminal handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: RouteListShells, Handle: handleListShells},
	})
	apischema.AttachDuplex(router, apischema.DuplexBinding{
		Route: RouteOpen,
		Handle: func(ctx context.Context, stream net.Conn, req apischema.TerminalOpenRequest) error {
			return HandleTerminalSession(ctx, rt, stream, req)
		},
	})
	apischema.AttachDuplex(router, apischema.DuplexBinding{
		Route: RouteContainerOpen,
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
