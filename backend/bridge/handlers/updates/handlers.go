package updates

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers dbus handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime) {
	RegisterJobRunners()

	rpc.Register("dbus", rt, []rpc.Command{
		{Name: "get_updates_basic", Handler: handleGetUpdatesBasic},
		{Name: "get_update_detail", Handler: handleGetUpdateDetail},
		{Name: "install_package", Handler: handleInstallPackage},
		{Name: "get_auto_updates", Handler: handleGetAutoUpdates},
		{Name: "set_auto_updates", Handler: handleSetAutoUpdates},
		{Name: "apply_offline_updates", Handler: handleApplyOfflineUpdates},
		{Name: "get_update_history", Handler: handleGetUpdateHistory},
	})
}

func handleGetUpdatesBasic(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetUpdatesBasic(ctx)
	return rpc.EmitResult(emit, result, err)
}

func handleGetUpdateDetail(ctx context.Context, args []string, emit ipc.Events) error {
	packageID, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := GetSingleUpdateDetail(ctx, packageID)
	return rpc.EmitResult(emit, result, err)
}

func handleInstallPackage(ctx context.Context, args []string, emit ipc.Events) error {
	packageName, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("install_package requested", "component", "dbus", "package", packageName)
	return rpc.EmitResult(emit, nil, InstallPackage(ctx, packageName))
}

func handleGetAutoUpdates(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := getAutoUpdates(ctx)
	return rpc.EmitResult(emit, result, err)
}

func handleSetAutoUpdates(ctx context.Context, args []string, emit ipc.Events) error {
	opts, err := rpc.DecodeJSONArg[AutoUpdateOptions](args, 0)
	if err != nil {
		return err
	}
	slog.Info("set_auto_updates requested", "component", "dbus", "mode", args[0])
	result, err := setAutoUpdates(ctx, opts)
	return rpc.EmitResult(emit, result, err)
}

func handleApplyOfflineUpdates(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("apply_offline_updates requested")
	result, err := applyOfflineUpdates(ctx)
	return rpc.EmitResult(emit, result, err)
}

func handleGetUpdateHistory(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetUpdateHistory()
	return rpc.EmitResult(emit, result, err)
}
