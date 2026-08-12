package hostname

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Call[apischema.HostnameRequest, apischema.NoResponse]("hostname.set_hostname").HandleVoid(handleSetHostname),
)

var Routes = api.Routes()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleSetHostname(ctx context.Context, req apischema.HostnameRequest) error {
	return SetHostname(ctx, req.Hostname)
}
