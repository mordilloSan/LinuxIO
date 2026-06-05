package hostname

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Job("hostname.set_hostname", apischema.TypeOf[apischema.HostnameRequest](), apischema.NoResponse()).Handle(handleSetHostname),
)

var Routes = api.Routes()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleSetHostname(ctx context.Context, req apischema.HostnameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, SetHostname(ctx, req.Hostname))
}
