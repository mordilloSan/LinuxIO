package filebrowser

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Call[apischema.FileResourceGetRequest, apischema.ExtendedFileInfo]("filebrowser.resource_get", apischema.RetrySafe()).Handle(handleResourceGet),
	apischema.Call[apischema.PathRequest, *apischema.ResourceStatData]("filebrowser.resource_stat", apischema.RetrySafe()).Handle(handleResourceStat),
	apischema.Call[apischema.BatchPathRequest, apischema.ExistsBatchResponse]("filebrowser.exists_batch", apischema.RetrySafe()).Handle(handleExistsBatch),
	apischema.Call[apischema.FileResourcePostRequest, apischema.NoResponse]("filebrowser.resource_post").HandleVoid(handleResourcePost),
	apischema.TaskRunner[apischema.ActionSourceDestinationRequest, FileOperationResult]("filebrowser.resource_patch", apischema.SessionTask(), apischema.WithTaskProgress[FileProgress]()).Run(handleResourcePatch, bridgeipc.TaskDefault),
	apischema.Call[apischema.PathRequest, apischema.DirectorySizeData]("filebrowser.dir_size", apischema.RetrySafe()).Handle(handleDirSize),
	apischema.Call[apischema.NoRequest, apischema.IndexerStatusResponse]("filebrowser.indexer_status", apischema.RetrySafe()).Handle(handleIndexerStatus),
	apischema.Call[apischema.PathRequest, apischema.SubfoldersResponse]("filebrowser.subfolders", apischema.RetrySafe()).Handle(handleSubfolders),
	apischema.Call[apischema.FileSearchRequest, apischema.SearchResponse]("filebrowser.search", apischema.RetrySafe()).Handle(handleSearch),
	apischema.Call[apischema.NoRequest, apischema.UsersGroupsResponse]("filebrowser.users_groups", apischema.RetrySafe()).Handle(handleUsersGroups),
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

func handleResourcePatch(ctx context.Context, task *bridgeipc.Task, req apischema.ActionSourceDestinationRequest) (FileOperationResult, error) {
	return resourcePatchWithProgress(ctx, req, task)
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
