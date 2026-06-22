package filebrowser

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Query[apischema.FileResourceGetRequest, apischema.ExtendedFileInfo]("filebrowser.resource_get").Handle(handleResourceGet),
	apischema.Query[apischema.PathRequest, apischema.ResourceStatData]("filebrowser.resource_stat").Handle(handleResourceStat),
	apischema.Job[apischema.PathRequest, apischema.JobSnapshot]("filebrowser.resource_delete").Handle(handleResourceDelete),
	apischema.Job[apischema.FileResourcePostRequest, apischema.NoResponse]("filebrowser.resource_post").Handle(handleResourcePost),
	apischema.Job[apischema.ActionSourceDestinationRequest, apischema.NoResponse]("filebrowser.resource_patch").Handle(handleResourcePatch),
	apischema.Query[apischema.PathRequest, apischema.DirectorySizeData]("filebrowser.dir_size").Handle(handleDirSize),
	apischema.Query[apischema.NoRequest, apischema.IndexerStatusResponse]("filebrowser.indexer_status").Handle(handleIndexerStatus),
	apischema.Query[apischema.PathRequest, apischema.SubfoldersResponse]("filebrowser.subfolders").Handle(handleSubfolders),
	apischema.Query[apischema.FileSearchRequest, apischema.SearchResponse]("filebrowser.search").Handle(handleSearch),
	apischema.Query[apischema.NoRequest, apischema.UsersGroupsResponse]("filebrowser.users_groups").Handle(handleUsersGroups),
)

var Routes = apischema.CombineRoutes(api.Routes(), fileJobRoutes)

// RegisterHandlers registers all filebrowser handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	RegisterJobRoutes(router, rt.Store)

	api.Register(router)
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

func handleIndexerStatus(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
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

func handleUsersGroups(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := usersGroups(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}
