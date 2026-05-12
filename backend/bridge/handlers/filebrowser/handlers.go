package filebrowser

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers all filebrowser handlers with the global registry
func RegisterHandlers(rt runtime.Runtime) {
	store := rt.Store
	RegisterJobRunners(store)

	rpc.Register("filebrowser", rt, []rpc.Command{
		{Name: "resource_get", Handler: handleResourceGet},
		{Name: "resource_stat", Handler: handleResourceStat},
		{Name: "resource_delete", Handler: handleResourceDelete},
		{Name: "resource_post", Handler: handleResourcePost},
		{Name: "resource_patch", Handler: handleResourcePatch},
		{Name: "dir_size", Handler: handleDirSize},
		{Name: "indexer_status", Handler: handleIndexerStatus},
		{Name: "subfolders", Handler: handleSubfolders},
		{Name: "search", Handler: handleSearch},
		{Name: "users_groups", Handler: handleUsersGroups},
	})
}

func handleResourceGet(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := resourceGet(args)
	return rpc.EmitResult(emit, result, err)
}

func handleResourceStat(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := resourceStat(args)
	return rpc.EmitResult(emit, result, err)
}

func handleResourceDelete(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("resource_delete requested", "component", "filebrowser")
	result, err := resourceDelete(args)
	return rpc.EmitResult(emit, result, err)
}

func handleResourcePost(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("resource_post requested", "component", "filebrowser")
	result, err := resourcePost(args)
	return rpc.EmitResult(emit, result, err)
}

func handleResourcePatch(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("resource_patch requested")
	result, err := resourcePatchWithProgress(ctx, args, emit)
	return rpc.EmitResult(emit, result, err)
}

func handleDirSize(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := dirSize(args)
	return rpc.EmitResult(emit, result, err)
}

func handleIndexerStatus(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := indexerStatus(args)
	return rpc.EmitResult(emit, result, err)
}

func handleSubfolders(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := subfolders(args)
	return rpc.EmitResult(emit, result, err)
}

func handleSearch(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := searchFiles(args)
	return rpc.EmitResult(emit, result, err)
}

func handleUsersGroups(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := usersGroups()
	return rpc.EmitResult(emit, result, err)
}
