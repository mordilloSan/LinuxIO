package filebrowser

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Query("filebrowser.resource_get", apischema.TypeOf[apischema.FileResourceGetRequest](), apischema.TypeOf[apischema.ApiResource]()).Handle(handleResourceGet),
	apischema.Query("filebrowser.resource_stat", apischema.TypeOf[apischema.PathRequest](), apischema.TypeOf[apischema.ResourceStatData]()).Handle(handleResourceStat),
	apischema.Job("filebrowser.resource_delete", apischema.TypeOf[apischema.PathRequest](), apischema.TypeOf[apischema.JobSnapshot]()).Handle(handleResourceDelete),
	apischema.Job("filebrowser.resource_post", apischema.TypeOf[apischema.FileResourcePostRequest](), apischema.NoResponse()).Handle(handleResourcePost),
	apischema.Job("filebrowser.resource_patch", apischema.TypeOf[apischema.ActionSourceDestinationRequest](), apischema.NoResponse()).Handle(handleResourcePatch),
	apischema.Query("filebrowser.dir_size", apischema.TypeOf[apischema.PathRequest](), apischema.TypeOf[apischema.DirectorySizeData]()).Handle(handleDirSize),
	apischema.Query("filebrowser.indexer_status", apischema.NoRequest(), apischema.TypeOf[apischema.IndexerStatusResponse]()).Handle(handleIndexerStatus),
	apischema.Query("filebrowser.subfolders", apischema.TypeOf[apischema.PathRequest](), apischema.TypeOf[apischema.SubfoldersResponse]()).Handle(handleSubfolders),
	apischema.Query("filebrowser.search", apischema.TypeOf[apischema.FileSearchRequest](), apischema.TypeOf[apischema.SearchResponse]()).Handle(handleSearch),
	apischema.Query("filebrowser.users_groups", apischema.NoRequest(), apischema.TypeOf[apischema.UsersGroupsResponse]()).Handle(handleUsersGroups),
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
