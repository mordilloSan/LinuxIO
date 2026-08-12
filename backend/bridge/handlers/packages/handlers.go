package packages

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Call[apischema.NoRequest, []apischema.Update]("updates.get_updates_basic", apischema.RetrySafe()).Handle(handleGetUpdatesBasic),
	apischema.Call[apischema.PackageIDRequest, apischema.Update]("updates.get_update_detail", apischema.RetrySafe()).Handle(handleGetUpdateDetail),
	apischema.Call[apischema.NoRequest, apischema.AutoUpdateState]("updates.get_auto_updates", apischema.RetrySafe()).Handle(handleGetAutoUpdates),
	apischema.Call[apischema.UpdatesSetAutoUpdatesRequest, apischema.AutoUpdateState]("updates.set_auto_updates").Handle(handleSetAutoUpdates),
	apischema.Call[apischema.NoRequest, apischema.OfflineUpdatesResponse]("updates.apply_offline_updates").Handle(handleApplyOfflineUpdates),
	apischema.Call[apischema.NoRequest, apischema.SuccessResponse]("updates.refresh_cache").Handle(handleRefreshUpdateCache),
	apischema.Call[apischema.NoRequest, []apischema.UpdateHistoryRow]("updates.get_update_history", apischema.RetrySafe()).Handle(handleGetUpdateHistory),
)

var Routes = apischema.CombineRoutes(api.Routes(), packageUpdateRoutes, capabilityInstallRoutes)

// RegisterHandlers registers package + update handlers with the IPC router.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	RegisterTaskRoutes(router)
	RegisterCapabilityTaskRoutes(router)

	api.Register(router)
}

func handleGetUpdatesBasic(ctx context.Context, _ apischema.NoRequest) ([]apischema.Update, error) {
	result, err := GetUpdatesBasic(ctx)
	return updatesToAPI(result), err
}

func handleGetUpdateDetail(ctx context.Context, req apischema.PackageIDRequest) (apischema.Update, error) {
	result, err := getSingleUpdateDetail(ctx, req.PackageID)
	if err != nil {
		return apischema.Update{}, err
	}
	return updateToAPI(*result), nil
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
