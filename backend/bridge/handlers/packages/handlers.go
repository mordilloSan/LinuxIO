package packages

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	packagesapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/packages/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers package + update handlers with the IPC router.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	RegisterJobRoutes(router)
	RegisterCapabilityJobRoutes(router)

	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: packagesapi.UpdatesGetUpdatesBasic, Handle: handleGetUpdatesBasic},
		{Route: packagesapi.UpdatesGetUpdateDetail, Handle: handleGetUpdateDetail},
		{Route: packagesapi.UpdatesInstallPackage, Handle: handleInstallPackage},
		{Route: packagesapi.UpdatesGetAutoUpdates, Handle: handleGetAutoUpdates},
		{Route: packagesapi.UpdatesSetAutoUpdates, Handle: handleSetAutoUpdates},
		{Route: packagesapi.UpdatesApplyOfflineUpdates, Handle: handleApplyOfflineUpdates},
		{Route: packagesapi.UpdatesGetUpdateHistory, Handle: handleGetUpdateHistory},
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
