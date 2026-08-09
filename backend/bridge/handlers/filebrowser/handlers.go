package filebrowser

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Call[apischema.FileResourceGetRequest, apischema.ExtendedFileInfo]("filebrowser.resource_get").Handle(handleResourceGet),
	apischema.Call[apischema.PathRequest, *apischema.ResourceStatData]("filebrowser.resource_stat").Handle(handleResourceStat),
	apischema.Call[apischema.BatchPathRequest, apischema.ExistsBatchResponse]("filebrowser.exists_batch").Handle(handleExistsBatch),
	apischema.Call[apischema.FileResourcePostRequest, apischema.NoResponse]("filebrowser.resource_post").HandleVoid(handleResourcePost),
	apischema.Task[apischema.ActionSourceDestinationRequest, apischema.NoResponse]("filebrowser.resource_patch").HandleEvents(handleResourcePatch),
	apischema.Call[apischema.PathRequest, apischema.DirectorySizeData]("filebrowser.dir_size").Handle(handleDirSize),
	apischema.Call[apischema.NoRequest, apischema.IndexerStatusResponse]("filebrowser.indexer_status").Handle(handleIndexerStatus),
	apischema.Call[apischema.PathRequest, apischema.SubfoldersResponse]("filebrowser.subfolders").Handle(handleSubfolders),
	apischema.Call[apischema.FileSearchRequest, apischema.SearchResponse]("filebrowser.search").Handle(handleSearch),
	apischema.Call[apischema.NoRequest, apischema.UsersGroupsResponse]("filebrowser.users_groups").Handle(handleUsersGroups),
)

var Routes = apischema.CombineRoutes(api.Routes(), fileTaskRoutes)

// RegisterHandlers registers all filebrowser handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	RegisterTaskRoutes(router, rt.Store)

	api.Register(router)
}

func handleResourceGet(ctx context.Context, req apischema.FileResourceGetRequest) (apischema.ExtendedFileInfo, error) {
	return resourceGet(ctx, req)
}

func handleResourceStat(ctx context.Context, req apischema.PathRequest) (*apischema.ResourceStatData, error) {
	return resourceStat(ctx, req)
}

func handleExistsBatch(ctx context.Context, req apischema.BatchPathRequest) (apischema.ExistsBatchResponse, error) {
	return existsBatch(ctx, req)
}

func handleResourcePost(ctx context.Context, req apischema.FileResourcePostRequest) error {
	_, err := resourcePost(ctx, req)
	return err
}

func handleResourcePatch(ctx context.Context, req apischema.ActionSourceDestinationRequest, emit bridgeipc.Events) error {
	result, err := resourcePatchWithProgress(ctx, req, emit)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleDirSize(ctx context.Context, req apischema.PathRequest) (apischema.DirectorySizeData, error) {
	return dirSize(ctx, req)
}

func handleIndexerStatus(ctx context.Context, _ apischema.NoRequest) (apischema.IndexerStatusResponse, error) {
	result, err := indexerStatus(ctx)
	if err != nil {
		return apischema.IndexerStatusResponse{}, err
	}
	return indexerStatusToAPI(result), nil
}

func handleSubfolders(ctx context.Context, req apischema.PathRequest) (apischema.SubfoldersResponse, error) {
	return subfolders(ctx, req)
}

func handleSearch(ctx context.Context, req apischema.FileSearchRequest) (apischema.SearchResponse, error) {
	return searchFiles(ctx, req)
}

func handleUsersGroups(ctx context.Context, _ apischema.NoRequest) (apischema.UsersGroupsResponse, error) {
	return usersGroups(ctx)
}
