package appupdate

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var Routes = routeBindings().Routes()

func routeBindings() apischema.BindingSet {
	policy := bridgeipc.TaskSingletonSystem
	return apischema.Bindings(
		apischema.Call[apischema.NoRequest, apischema.VersionResponse]("control.version", apischema.RetrySafe()).Handle(handleVersion),
		apischema.TaskRunner[apischema.AppUpdateRequest, AppUpdateResult](
			routeAppUpdate,
			apischema.NoEndpoint(),
			apischema.DurableTask(),
			apischema.WithTaskProgress[AppUpdateProgressDetail](),
			apischema.WithTaskIdentity(appUpdateTaskIdentity),
		).Run(runAppUpdateTask, policy),
	)
}

// RegisterHandlers registers app update handlers.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	routeBindings().Register(router)
	recoverAppUpdates(rt, router)
}

func handleVersion(ctx context.Context, _ apischema.NoRequest) (apischema.VersionResponse, error) {
	return getVersionInfo(ctx)
}
