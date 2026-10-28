package hostname

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
)

var api = apischema.Bindings(
	apischema.MutationVoid("hostname.set_hostname", handleSetHostname),
)

var Routes = api.Routes()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleSetHostname(ctx context.Context, req apischema.HostnameRequest) error {
	return SetHostname(ctx, req.Hostname)
}
