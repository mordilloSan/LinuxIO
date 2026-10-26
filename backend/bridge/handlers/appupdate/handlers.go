package appupdate

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
)

var Routes = routeBindings(runtime.Runtime{}).Routes()

func routeBindings(rt runtime.Runtime) apischema.BindingSet {
	return apischema.Bindings(
		apischema.Call[apischema.NoRequest, apischema.VersionResponse]("control.version", apischema.RetrySafe()).Handle(handleVersion),
		apischema.TaskRunner[apischema.AppUpdateRequest, AppUpdateResult](routeAppUpdate, apischema.NoEndpoint(), apischema.SessionTask(), apischema.WithTaskProgress[AppUpdateProgressDetail]()).Run(
			func(ctx context.Context, task *bridgeipc.Task, req apischema.AppUpdateRequest) (AppUpdateResult, error) {
				return runAppUpdateTask(ctx, rt, task, req)
			},
			bridgeipc.TaskExclusive,
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
