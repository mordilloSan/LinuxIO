package packages

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routes = apischema.NewRouteCatalog()

var RouteUpdate = routes.Runner("packages.update", apischema.TypeOf[apischema.PackageUpdateRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var RouteUpdatesApplyOfflineUpdates = routes.Job("updates.apply_offline_updates", apischema.NoRequest(), apischema.TypeOf[apischema.OfflineUpdatesResponse]())
var RouteUpdatesGetAutoUpdates = routes.Query("updates.get_auto_updates", apischema.NoRequest(), apischema.TypeOf[apischema.AutoUpdateState]())
var RouteUpdatesGetUpdateDetail = routes.Query("updates.get_update_detail", apischema.TypeOf[apischema.PackageIDRequest](), apischema.TypeOf[apischema.Update]())
var RouteUpdatesGetUpdateHistory = routes.Query("updates.get_update_history", apischema.NoRequest(), apischema.TypeOf[[]apischema.UpdateHistoryRow]())
var RouteUpdatesGetUpdatesBasic = routes.Query("updates.get_updates_basic", apischema.NoRequest(), apischema.TypeOf[[]apischema.Update]())
var RouteUpdatesInstallPackage = routes.Job("updates.install_package", apischema.TypeOf[apischema.PackageIDRequest](), apischema.NoResponse())
var RouteUpdatesSetAutoUpdates = routes.Job("updates.set_auto_updates", apischema.TypeOf[apischema.UpdatesSetAutoUpdatesRequest](), apischema.TypeOf[apischema.AutoUpdateState]())

var Routes = routes.All()

// RegisterHandlers registers package + update handlers with the IPC router.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	RegisterJobRoutes(router)
	RegisterCapabilityJobRoutes(router)

	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: RouteUpdatesGetUpdatesBasic, Handle: handleGetUpdatesBasic},
		{Route: RouteUpdatesGetUpdateDetail, Handle: handleGetUpdateDetail},
		{Route: RouteUpdatesInstallPackage, Handle: handleInstallPackage},
		{Route: RouteUpdatesGetAutoUpdates, Handle: handleGetAutoUpdates},
		{Route: RouteUpdatesSetAutoUpdates, Handle: handleSetAutoUpdates},
		{Route: RouteUpdatesApplyOfflineUpdates, Handle: handleApplyOfflineUpdates},
		{Route: RouteUpdatesGetUpdateHistory, Handle: handleGetUpdateHistory},
	})
}

func handleGetUpdatesBasic(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := GetUpdatesBasic(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetUpdateDetail(ctx context.Context, req apischema.PackageIDRequest, emit bridgeipc.Events) error {
	result, err := getSingleUpdateDetail(ctx, req.PackageID)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleInstallPackage(ctx context.Context, req apischema.PackageIDRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, InstallPackage(ctx, req.PackageID))
}

func handleGetAutoUpdates(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := getAutoUpdates(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleSetAutoUpdates(ctx context.Context, req apischema.UpdatesSetAutoUpdatesRequest, emit bridgeipc.Events) error {
	result, err := setAutoUpdates(ctx, AutoUpdateOptions{
		Enabled:         req.Enabled,
		Frequency:       apischema.AutoUpdateFrequency(req.Frequency),
		Scope:           apischema.AutoUpdateScope(req.Scope),
		DownloadOnly:    req.DownloadOnly,
		RebootPolicy:    apischema.AutoUpdateRebootPolicy(req.RebootPolicy),
		ExcludePackages: req.ExcludePackages,
	})
	return bridgeipc.EmitResult(emit, result, err)
}

func handleApplyOfflineUpdates(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := applyOfflineUpdates(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetUpdateHistory(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := GetUpdateHistory(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}
