package updates

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers dbus handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	RegisterJobRoutes(router)

	bridgeipc.RegisterRoutes(router, "dbus", []bridgeipc.Command{
		{Name: "get_updates_basic", Mode: bridgeipc.ModeQuery, Handler: handleGetUpdatesBasic},
		{Name: "get_update_detail", Mode: bridgeipc.ModeQuery, Handler: handleGetUpdateDetail},
		{Name: "install_package", Mode: bridgeipc.ModeJob, Handler: handleInstallPackage},
		{Name: "get_auto_updates", Mode: bridgeipc.ModeQuery, Handler: handleGetAutoUpdates},
		{Name: "set_auto_updates", Mode: bridgeipc.ModeJob, Handler: handleSetAutoUpdates},
		{Name: "apply_offline_updates", Mode: bridgeipc.ModeJob, Handler: handleApplyOfflineUpdates},
		{Name: "get_update_history", Mode: bridgeipc.ModeQuery, Handler: handleGetUpdateHistory},
	})
}

func handleGetUpdatesBasic(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := GetUpdatesBasic(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetUpdateDetail(ctx context.Context, args []string, emit bridgeipc.Events) error {
	packageID, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := GetSingleUpdateDetail(ctx, packageID)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleInstallPackage(ctx context.Context, args []string, emit bridgeipc.Events) error {
	packageName, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, nil, InstallPackage(ctx, packageName))
}

func handleGetAutoUpdates(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := getAutoUpdates(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleSetAutoUpdates(ctx context.Context, args []string, emit bridgeipc.Events) error {
	opts, err := bridgeipc.DecodeJSONArg[AutoUpdateOptions](args, 0)
	if err != nil {
		return err
	}
	result, err := setAutoUpdates(ctx, opts)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleApplyOfflineUpdates(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := applyOfflineUpdates(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetUpdateHistory(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := GetUpdateHistory(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}
