package filebrowser

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	filebrowserapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers all filebrowser handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	RegisterJobRoutes(router, rt.Store)

	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: filebrowserapi.ResourceGet, Handle: handleResourceGet},
		{Route: filebrowserapi.ResourceStat, Handle: handleResourceStat},
		{Route: filebrowserapi.ResourceDelete, Handle: handleResourceDelete},
		{Route: filebrowserapi.ResourcePost, Handle: handleResourcePost},
		{Route: filebrowserapi.ResourcePatch, Handle: handleResourcePatch},
		{Route: filebrowserapi.DirSize, Handle: handleDirSize},
		{Route: filebrowserapi.IndexerStatus, Handle: handleIndexerStatus},
		{Route: filebrowserapi.Subfolders, Handle: handleSubfolders},
		{Route: filebrowserapi.Search, Handle: handleSearch},
		{Route: filebrowserapi.UsersGroups, Handle: handleUsersGroups},
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
