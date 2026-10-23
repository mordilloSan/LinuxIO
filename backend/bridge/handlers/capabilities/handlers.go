package capabilities

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

var Routes = routeBindings(runtime.Runtime{}).Routes()

func routeBindings(rt runtime.Runtime) apischema.BindingSet {
	handlers := capabilityHandlers{rt: rt}
	return apischema.Bindings(
		apischema.Call[apischema.NoRequest, apischema.CapabilitiesResponse]("system.get_capabilities", apischema.RetrySafe()).Handle(handlers.handleGetCapabilities),
	)
}

// RegisterHandlers registers the capability routes with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	routeBindings(rt).Register(router)
}

func (h capabilityHandlers) handleGetCapabilities(ctx context.Context, _ apischema.NoRequest) (apischema.CapabilitiesResponse, error) {
	var user session.User
	if h.rt.Session != nil {
		user = h.rt.Session.User
	}
	return buildCapabilitiesResponse(ctx, user)
}
