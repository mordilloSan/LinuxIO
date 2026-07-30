package packages

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Query[apischema.NoRequest, []apischema.Update]("updates.get_updates_basic").HandleEvents(handleGetUpdatesBasic),
	apischema.Query[apischema.PackageIDRequest, apischema.Update]("updates.get_update_detail").HandleEvents(handleGetUpdateDetail),
	apischema.Query[apischema.NoRequest, apischema.AutoUpdateState]("updates.get_auto_updates").Handle(handleGetAutoUpdates),
	apischema.Job[apischema.UpdatesSetAutoUpdatesRequest, apischema.AutoUpdateState]("updates.set_auto_updates").Handle(handleSetAutoUpdates),
	apischema.Job[apischema.NoRequest, apischema.OfflineUpdatesResponse]("updates.apply_offline_updates").Handle(handleApplyOfflineUpdates),
	apischema.Job[apischema.NoRequest, apischema.SuccessResponse]("updates.refresh_cache").Handle(handleRefreshUpdateCache),
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

func handleGetAutoUpdates(ctx context.Context, _ apischema.NoRequest) (apischema.AutoUpdateState, error) {
	return getAutoUpdates(ctx)
}

func handleSetAutoUpdates(ctx context.Context, req apischema.UpdatesSetAutoUpdatesRequest) (apischema.AutoUpdateState, error) {
	return setAutoUpdates(ctx, AutoUpdateOptions{
		Enabled:         req.Enabled,
		Frequency:       apischema.AutoUpdateFrequency(req.Frequency),
		Scope:           apischema.AutoUpdateScope(req.Scope),
		DownloadOnly:    req.DownloadOnly,
		RebootPolicy:    apischema.AutoUpdateRebootPolicy(req.RebootPolicy),
		ExcludePackages: req.ExcludePackages,
	})
}

func handleApplyOfflineUpdates(ctx context.Context, _ apischema.NoRequest) (apischema.OfflineUpdatesResponse, error) {
	return applyOfflineUpdates(ctx)
}

func handleRefreshUpdateCache(ctx context.Context, _ apischema.NoRequest) (apischema.SuccessResponse, error) {
	if err := RefreshUpdateCache(ctx); err != nil {
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}

func handleGetUpdateHistory(ctx context.Context, _ apischema.NoRequest) ([]apischema.UpdateHistoryRow, error) {
	return GetUpdateHistory(ctx)
}
