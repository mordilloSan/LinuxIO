package hostname

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, "hostname", []bridgeipc.Command{
		{Name: "set_hostname", Mode: bridgeipc.ModeJob, Handler: handleSetHostname},
	})
}

func handleSetHostname(ctx context.Context, req apischema.HostnameRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, SetHostname(ctx, req.Hostname))
}
