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
	policy := bridgeipc.TaskSingletonSystem
	policy.Timeout = 30 * time.Minute
	return apischema.Bindings(
		apischema.Call[apischema.NoRequest, apischema.VersionResponse]("control.version").Handle(handleVersion),
		apischema.TaskRunner[apischema.AppUpdateRequest, apischema.NoResponse](routeAppUpdate, apischema.NoEndpoint()).Run(
			func(ctx context.Context, task *bridgeipc.Task, req apischema.AppUpdateRequest) (any, error) {
				return runAppUpdateTask(ctx, rt, task, req)
			},
			policy,
		),
	)
}

// RegisterHandlers registers app update handlers.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	routeBindings(rt).Register(router)
}

func handleVersion(ctx context.Context, _ apischema.NoRequest) (apischema.VersionResponse, error) {
	return getVersionInfo(ctx)
}
