package packages

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Query[apischema.NoRequest, []apischema.Update]("updates.get_updates_basic").Handle(handleGetUpdatesBasic),
	apischema.Query[apischema.PackageIDRequest, apischema.Update]("updates.get_update_detail").Handle(handleGetUpdateDetail),
	apischema.Job[apischema.PackageIDRequest, apischema.NoResponse]("updates.install_package").Handle(handleInstallPackage),
	apischema.Query[apischema.NoRequest, apischema.AutoUpdateState]("updates.get_auto_updates").Handle(handleGetAutoUpdates),
	apischema.Job[apischema.UpdatesSetAutoUpdatesRequest, apischema.AutoUpdateState]("updates.set_auto_updates").Handle(handleSetAutoUpdates),
	apischema.Job[apischema.NoRequest, apischema.OfflineUpdatesResponse]("updates.apply_offline_updates").Handle(handleApplyOfflineUpdates),
	apischema.Query[apischema.NoRequest, []apischema.UpdateHistoryRow]("updates.get_update_history").Handle(handleGetUpdateHistory),
)

var Routes = apischema.CombineRoutes(api.Routes(), packageUpdateRoutes, capabilityInstallRoutes)

// RegisterHandlers registers package + update handlers with the IPC router.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	RegisterJobRoutes(router)
	RegisterCapabilityJobRoutes(router)

	api.Register(router)
}

func handleGetUpdatesBasic(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
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

func handleGetAutoUpdates(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
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

func handleApplyOfflineUpdates(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := applyOfflineUpdates(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetUpdateHistory(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := GetUpdateHistory(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}
