package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var Archive = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "filebrowser.archive", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.FileArchiveRequest](), Result: apischema.TypeOf[apischema.JobSnapshot]()}
var Chmod = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "filebrowser.chmod", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.FileChmodRequest](), Result: apischema.TypeOf[apischema.JobSnapshot]()}
var Compress = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "filebrowser.compress", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.FileCompressRequest](), Result: apischema.TypeOf[apischema.JobSnapshot]()}
var Copy = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "filebrowser.copy", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.SourceDestinationRequest](), Result: apischema.TypeOf[apischema.JobSnapshot]()}
var DirSize = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "filebrowser.dir_size", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.PathRequest](), Result: apischema.TypeOf[apischema.DirectorySizeData]()}
var Download = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "filebrowser.download", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.PathRequest](), Result: apischema.TypeOf[apischema.JobSnapshot]()}
var Extract = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "filebrowser.extract", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.FileExtractRequest](), Result: apischema.TypeOf[apischema.JobSnapshot]()}
var Index = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "filebrowser.index", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.OptionalPathRequest](), Result: apischema.TypeOf[apischema.JobSnapshot]()}
var IndexerStatus = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "filebrowser.indexer_status", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.IndexerStatusResponse]()}
var Move = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "filebrowser.move", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.SourceDestinationRequest](), Result: apischema.TypeOf[apischema.JobSnapshot]()}
var ResourceDelete = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "filebrowser.resource_delete", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.PathRequest](), Result: apischema.TypeOf[apischema.JobSnapshot]()}
var ResourceGet = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "filebrowser.resource_get", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.FileResourceGetRequest](), Result: apischema.TypeOf[apischema.ApiResource]()}
var ResourcePatch = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "filebrowser.resource_patch", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ActionSourceDestinationRequest](), Result: apischema.NoResponse()}
var ResourcePost = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "filebrowser.resource_post", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.FileResourcePostRequest](), Result: apischema.NoResponse()}
var ResourceStat = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "filebrowser.resource_stat", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.PathRequest](), Result: apischema.TypeOf[apischema.ResourceStatData]()}
var Search = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "filebrowser.search", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.FileSearchRequest](), Result: apischema.TypeOf[apischema.SearchResponse]()}
var Subfolders = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "filebrowser.subfolders", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.PathRequest](), Result: apischema.TypeOf[apischema.SubfoldersResponse]()}
var Upload = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "filebrowser.upload", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.FileUploadRequest](), Result: apischema.TypeOf[apischema.JobSnapshot]()}
var UsersGroups = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "filebrowser.users_groups", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.UsersGroupsResponse]()}

var Routes = []apischema.RouteSpec{
	Archive,
	Chmod,
	Compress,
	Copy,
	DirSize,
	Download,
	Extract,
	Index,
	IndexerStatus,
	Move,
	ResourceDelete,
	ResourceGet,
	ResourcePatch,
	ResourcePost,
	ResourceStat,
	Search,
	Subfolders,
	Upload,
	UsersGroups,
}
