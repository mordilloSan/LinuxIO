package packages

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers package + update handlers with the IPC router.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	RegisterJobRoutes(router)
	RegisterCapabilityJobRoutes(router)

	apischema.RegisterRoutes(router, "updates", []bridgeipc.Command{
		{Name: "get_updates_basic", Mode: bridgeipc.ModeQuery, Handler: handleGetUpdatesBasic},
		{Name: "get_update_detail", Mode: bridgeipc.ModeQuery, Handler: handleGetUpdateDetail},
		{Name: "install_package", Mode: bridgeipc.ModeJob, Handler: handleInstallPackage},
		{Name: "get_auto_updates", Mode: bridgeipc.ModeQuery, Handler: handleGetAutoUpdates},
		{Name: "set_auto_updates", Mode: bridgeipc.ModeJob, Handler: handleSetAutoUpdates},
		{Name: "apply_offline_updates", Mode: bridgeipc.ModeJob, Handler: handleApplyOfflineUpdates},
		{Name: "get_update_history", Mode: bridgeipc.ModeQuery, Handler: handleGetUpdateHistory},
	})
}

func handleGetUpdatesBasic(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := GetUpdatesBasic(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetUpdateDetail(ctx context.Context, req apischema.PackageIDRequest, emit bridgeipc.Events) error {
	result, err := GetSingleUpdateDetail(ctx, req.PackageID)
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
