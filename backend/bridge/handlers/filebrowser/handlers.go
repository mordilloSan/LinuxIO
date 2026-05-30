package filebrowser

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers all filebrowser handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	store := rt.Store
	RegisterJobRoutes(router, store)

	apischema.RegisterRoutes(router, "filebrowser", []bridgeipc.Command{
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

func handleResourceGet(ctx context.Context, req apischema.FileResourceGetRequest, emit bridgeipc.Events) error {
	result, err := resourceGet(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleResourceStat(ctx context.Context, req apischema.PathRequest, emit bridgeipc.Events) error {
	result, err := resourceStat(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleResourceDelete(ctx context.Context, req apischema.PathRequest, emit bridgeipc.Events) error {
	result, err := resourceDelete(ctx, req, emit)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleResourcePost(ctx context.Context, req apischema.FileResourcePostRequest, emit bridgeipc.Events) error {
	result, err := resourcePost(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleResourcePatch(ctx context.Context, req apischema.ActionSourceDestinationRequest, emit bridgeipc.Events) error {
	result, err := resourcePatchWithProgress(ctx, req, emit)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleDirSize(ctx context.Context, req apischema.PathRequest, emit bridgeipc.Events) error {
	result, err := dirSize(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleIndexerStatus(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := indexerStatus(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleSubfolders(ctx context.Context, req apischema.PathRequest, emit bridgeipc.Events) error {
	result, err := subfolders(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleSearch(ctx context.Context, req apischema.FileSearchRequest, emit bridgeipc.Events) error {
	result, err := searchFiles(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleUsersGroups(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := usersGroups(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}
