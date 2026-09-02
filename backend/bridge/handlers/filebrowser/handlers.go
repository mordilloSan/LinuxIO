package filebrowser

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.DuplexRoute[apischema.PathRequest, apischema.NoResponse](routeDownloadStream, apischema.NoEndpoint()).Duplex(streamFileDownload),
	apischema.Call[apischema.PathRequest, apischema.DirectoryListing]("filebrowser.list_directory", apischema.RetrySafe()).Handle(handleListDirectory),
	apischema.Call[apischema.DirectoryChildrenRequest, apischema.DirectoryChildren]("filebrowser.directory_children", apischema.RetrySafe()).Handle(handleDirectoryChildren),
	apischema.Call[apischema.PathRequest, apischema.TextFile]("filebrowser.read_text", apischema.RetrySafe()).Handle(handleReadText),
	apischema.Call[apischema.PathRequest, *apischema.ResourceStatData]("filebrowser.resource_stat", apischema.RetrySafe()).Handle(handleResourceStat),
	apischema.Call[apischema.BatchPathRequest, apischema.ExistsBatchResponse]("filebrowser.exists_batch", apischema.RetrySafe()).Handle(handleExistsBatch),
	apischema.Call[apischema.FileResourcePostRequest, apischema.NoResponse]("filebrowser.resource_post").HandleVoid(handleResourcePost),
	apischema.TaskRunner[apischema.ActionSourceDestinationRequest, apischema.MessageResponse]("filebrowser.resource_patch", apischema.SessionTask(), apischema.WithTaskProgress[FileProgress]()).Run(handleResourcePatch, bridgeipc.TaskDefault),
	apischema.Call[apischema.PathRequest, apischema.DirectorySizeData]("filebrowser.dir_size", apischema.RetrySafe(), apischema.Privileged()).Handle(handleDirSize),
	apischema.Call[apischema.PathRequest, apischema.SubfoldersResponse]("filebrowser.subfolders", apischema.RetrySafe(), apischema.Privileged()).Handle(handleSubfolders),
	apischema.Call[apischema.FileSearchRequest, apischema.SearchResponse]("filebrowser.search", apischema.RetrySafe(), apischema.Privileged()).Handle(handleSearch),
	apischema.Call[apischema.NoRequest, apischema.UsersGroupsResponse]("filebrowser.users_groups", apischema.RetrySafe()).Handle(handleUsersGroups),
)

var Routes = apischema.CombineRoutes(api.Routes(), fileTaskRoutes)

// RegisterHandlers registers all filebrowser handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	RegisterTaskRoutes(router, rt.Store)

	api.Register(router)
}

func handleListDirectory(ctx context.Context, req apischema.PathRequest) (apischema.DirectoryListing, error) {
	return listDirectory(ctx, req)
}

func handleDirectoryChildren(ctx context.Context, req apischema.DirectoryChildrenRequest) (apischema.DirectoryChildren, error) {
	return directoryChildren(ctx, req)
}

func handleReadText(ctx context.Context, req apischema.PathRequest) (apischema.TextFile, error) {
	return readText(ctx, req)
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

func handleResourcePatch(ctx context.Context, task *bridgeipc.Task, req apischema.ActionSourceDestinationRequest) (apischema.MessageResponse, error) {
	return resourcePatchWithProgress(ctx, req, task)
}

func handleDirSize(ctx context.Context, req apischema.PathRequest) (apischema.DirectorySizeData, error) {
	return dirSize(ctx, req)
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
