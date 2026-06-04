package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var Archive = routes.Runner("filebrowser.archive", apischema.TypeOf[apischema.FileArchiveRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var Chmod = routes.Runner("filebrowser.chmod", apischema.TypeOf[apischema.FileChmodRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var Compress = routes.Runner("filebrowser.compress", apischema.TypeOf[apischema.FileCompressRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var Copy = routes.Runner("filebrowser.copy", apischema.TypeOf[apischema.SourceDestinationRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var DirSize = routes.Query("filebrowser.dir_size", apischema.TypeOf[apischema.PathRequest](), apischema.TypeOf[apischema.DirectorySizeData]())
var Download = routes.Runner("filebrowser.download", apischema.TypeOf[apischema.PathRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var Extract = routes.Runner("filebrowser.extract", apischema.TypeOf[apischema.FileExtractRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var Index = routes.Runner("filebrowser.index", apischema.TypeOf[apischema.OptionalPathRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var IndexerStatus = routes.Query("filebrowser.indexer_status", apischema.NoRequest(), apischema.TypeOf[apischema.IndexerStatusResponse]())
var Move = routes.Runner("filebrowser.move", apischema.TypeOf[apischema.SourceDestinationRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var ResourceDelete = routes.Job("filebrowser.resource_delete", apischema.TypeOf[apischema.PathRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var ResourceGet = routes.Query("filebrowser.resource_get", apischema.TypeOf[apischema.FileResourceGetRequest](), apischema.TypeOf[apischema.ApiResource]())
var ResourcePatch = routes.Job("filebrowser.resource_patch", apischema.TypeOf[apischema.ActionSourceDestinationRequest](), apischema.NoResponse())
var ResourcePost = routes.Job("filebrowser.resource_post", apischema.TypeOf[apischema.FileResourcePostRequest](), apischema.NoResponse())
var ResourceStat = routes.Query("filebrowser.resource_stat", apischema.TypeOf[apischema.PathRequest](), apischema.TypeOf[apischema.ResourceStatData]())
var Search = routes.Query("filebrowser.search", apischema.TypeOf[apischema.FileSearchRequest](), apischema.TypeOf[apischema.SearchResponse]())
var Subfolders = routes.Query("filebrowser.subfolders", apischema.TypeOf[apischema.PathRequest](), apischema.TypeOf[apischema.SubfoldersResponse]())
var Upload = routes.Runner("filebrowser.upload", apischema.TypeOf[apischema.FileUploadRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var UsersGroups = routes.Query("filebrowser.users_groups", apischema.NoRequest(), apischema.TypeOf[apischema.UsersGroupsResponse]())

var Routes = routes.All()
