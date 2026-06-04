package filebrowser

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routes = apischema.NewRouteCatalog()

var RouteArchive = routes.Runner("filebrowser.archive", apischema.TypeOf[apischema.FileArchiveRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var RouteChmod = routes.Runner("filebrowser.chmod", apischema.TypeOf[apischema.FileChmodRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var RouteCompress = routes.Runner("filebrowser.compress", apischema.TypeOf[apischema.FileCompressRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var RouteCopy = routes.Runner("filebrowser.copy", apischema.TypeOf[apischema.SourceDestinationRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var RouteDirSize = routes.Query("filebrowser.dir_size", apischema.TypeOf[apischema.PathRequest](), apischema.TypeOf[apischema.DirectorySizeData]())
var RouteDownload = routes.Runner("filebrowser.download", apischema.TypeOf[apischema.PathRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var RouteExtract = routes.Runner("filebrowser.extract", apischema.TypeOf[apischema.FileExtractRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var RouteIndex = routes.Runner("filebrowser.index", apischema.TypeOf[apischema.OptionalPathRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var RouteIndexerStatus = routes.Query("filebrowser.indexer_status", apischema.NoRequest(), apischema.TypeOf[apischema.IndexerStatusResponse]())
var RouteMove = routes.Runner("filebrowser.move", apischema.TypeOf[apischema.SourceDestinationRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var RouteResourceDelete = routes.Job("filebrowser.resource_delete", apischema.TypeOf[apischema.PathRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var RouteResourceGet = routes.Query("filebrowser.resource_get", apischema.TypeOf[apischema.FileResourceGetRequest](), apischema.TypeOf[apischema.ApiResource]())
var RouteResourcePatch = routes.Job("filebrowser.resource_patch", apischema.TypeOf[apischema.ActionSourceDestinationRequest](), apischema.NoResponse())
var RouteResourcePost = routes.Job("filebrowser.resource_post", apischema.TypeOf[apischema.FileResourcePostRequest](), apischema.NoResponse())
var RouteResourceStat = routes.Query("filebrowser.resource_stat", apischema.TypeOf[apischema.PathRequest](), apischema.TypeOf[apischema.ResourceStatData]())
var RouteSearch = routes.Query("filebrowser.search", apischema.TypeOf[apischema.FileSearchRequest](), apischema.TypeOf[apischema.SearchResponse]())
var RouteSubfolders = routes.Query("filebrowser.subfolders", apischema.TypeOf[apischema.PathRequest](), apischema.TypeOf[apischema.SubfoldersResponse]())
var RouteUpload = routes.Runner("filebrowser.upload", apischema.TypeOf[apischema.FileUploadRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var RouteUsersGroups = routes.Query("filebrowser.users_groups", apischema.NoRequest(), apischema.TypeOf[apischema.UsersGroupsResponse]())

var Routes = routes.All()

// RegisterHandlers registers all filebrowser handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	RegisterJobRoutes(router, rt.Store)

	apischema.RegisterRoutes(router,
		RouteResourceGet.Handle(handleResourceGet),
		RouteResourceStat.Handle(handleResourceStat),
		RouteResourceDelete.Handle(handleResourceDelete),
		RouteResourcePost.Handle(handleResourcePost),
		RouteResourcePatch.Handle(handleResourcePatch),
		RouteDirSize.Handle(handleDirSize),
		RouteIndexerStatus.Handle(handleIndexerStatus),
		RouteSubfolders.Handle(handleSubfolders),
		RouteSearch.Handle(handleSearch),
		RouteUsersGroups.Handle(handleUsersGroups),
	)
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
