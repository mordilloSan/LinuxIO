package assistant

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
		apischema.DuplexRoute[apischema.AssistantOpenRequest, apischema.NoResponse]("assistant.open", apischema.NoEndpoint()).Duplex(
			func(ctx context.Context, stream net.Conn, req apischema.AssistantOpenRequest) error {
				return HandleAssistantSession(ctx, rt, stream, req)
			},
		),
	)
}

// RegisterHandlers registers the assistant routes with the bridge router.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	routeBindings(rt).Register(router)
}
