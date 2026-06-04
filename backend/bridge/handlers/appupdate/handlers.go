package appupdate

import (
	"context"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/control"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routes = apischema.NewRouteCatalog()

var RouteControlAppUpdate = routes.Runner("control.app_update", apischema.TypeOf[apischema.AppUpdateRequest](), apischema.NoResponse(), apischema.NoEndpoint())

var Routes = routes.All()

// RegisterHandlers registers control handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router,
		control.RouteVersion.Handle(handleVersion),
	)
	policy := bridgeipc.SingletonSystem
	policy.Timeout = 30 * time.Minute
	apischema.AttachRunner(router, RouteControlAppUpdate.Run(func(ctx context.Context, job *bridgeipc.Job, req apischema.AppUpdateRequest) (any, error) {
		return runAppUpdateJob(ctx, rt, job, req)
	}, policy))
}

func handleVersion(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	info, err := getVersionInfo(ctx)
	return bridgeipc.EmitResult(emit, info, err)
}
