package filebrowser

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers all filebrowser handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	store := rt.Store
	RegisterJobRoutes(router, store)

	bridgeipc.RegisterRoutes(router, "filebrowser", []bridgeipc.Command{
		{Name: "resource_get", Mode: bridgeipc.ModeQuery, Handler: handleResourceGet},
		{Name: "resource_stat", Mode: bridgeipc.ModeQuery, Handler: handleResourceStat},
		{Name: "resource_delete", Mode: bridgeipc.ModeJob, Handler: handleResourceDelete},
		{Name: "resource_post", Mode: bridgeipc.ModeJob, Handler: handleResourcePost},
		{Name: "resource_patch", Mode: bridgeipc.ModeJob, Handler: handleResourcePatch},
		{Name: "dir_size", Mode: bridgeipc.ModeQuery, Handler: handleDirSize},
		{Name: "indexer_status", Mode: bridgeipc.ModeQuery, Handler: handleIndexerStatus},
		{Name: "subfolders", Mode: bridgeipc.ModeQuery, Handler: handleSubfolders},
		{Name: "search", Mode: bridgeipc.ModeQuery, Handler: handleSearch},
		{Name: "users_groups", Mode: bridgeipc.ModeQuery, Handler: handleUsersGroups},
	})
}

func handleResourceGet(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := resourceGet(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleResourceStat(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := resourceStat(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleResourceDelete(ctx context.Context, args []string, emit bridgeipc.Events) error {
	slog.Info("resource_delete requested", "component", "filebrowser")
	result, err := resourceDelete(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleResourcePost(ctx context.Context, args []string, emit bridgeipc.Events) error {
	slog.Info("resource_post requested", "component", "filebrowser")
	result, err := resourcePost(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleResourcePatch(ctx context.Context, args []string, emit bridgeipc.Events) error {
	slog.Info("resource_patch requested")
	result, err := resourcePatchWithProgress(ctx, args, emit)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleDirSize(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := dirSize(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleIndexerStatus(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := indexerStatus(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleSubfolders(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := subfolders(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleSearch(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := searchFiles(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleUsersGroups(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := usersGroups()
	return bridgeipc.EmitResult(emit, result, err)
}
