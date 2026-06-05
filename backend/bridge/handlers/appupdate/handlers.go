package appupdate

import (
	"context"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var Routes = routeBindings(runtime.Runtime{}).Routes()

func routeBindings(rt runtime.Runtime) apischema.BindingSet {
	policy := bridgeipc.SingletonSystem
	policy.Timeout = 30 * time.Minute
	return apischema.Bindings(
		apischema.Query("control.version", apischema.NoRequest(), apischema.TypeOf[apischema.VersionResponse]()).Handle(handleVersion),
		apischema.Runner(routeAppUpdate, apischema.TypeOf[apischema.AppUpdateRequest](), apischema.NoResponse(), apischema.NoEndpoint()).Run(
			func(ctx context.Context, job *bridgeipc.Job, req apischema.AppUpdateRequest) (any, error) {
				return runAppUpdateJob(ctx, rt, job, req)
			},
			policy,
		),
	)
}

// RegisterHandlers registers app update handlers.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	routeBindings(rt).Register(router)
}

func handleVersion(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	info, err := getVersionInfo(ctx)
	return bridgeipc.EmitResult(emit, info, err)
}
