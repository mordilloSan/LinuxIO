package hostname

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routes = apischema.NewRouteCatalog()

var RouteSetHostname = routes.Job("hostname.set_hostname", apischema.TypeOf[apischema.HostnameRequest](), apischema.NoResponse())

var Routes = routes.All()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router,
		RouteSetHostname.Handle(handleSetHostname),
	)
}

func handleSetHostname(ctx context.Context, req apischema.HostnameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, SetHostname(ctx, req.Hostname))
}
