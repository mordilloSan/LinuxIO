package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var CreateNFSShare = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "shares.create_nfs_share", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ShareNFSRequest](), Result: apischema.TypeOf[apischema.SuccessPathResponse]()}
var CreateSambaShare = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "shares.create_samba_share", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ShareSambaRequest](), Result: apischema.TypeOf[apischema.SuccessNameResponse]()}
var DeleteNFSShare = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "shares.delete_nfs_share", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.PathRequest](), Result: apischema.TypeOf[apischema.SuccessResponse]()}
var DeleteSambaShare = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "shares.delete_samba_share", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.NameRequest](), Result: apischema.TypeOf[apischema.SuccessResponse]()}
var ListNFSShares = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "shares.list_nfs_shares", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.NFSExport]()}
var ListSambaShares = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "shares.list_samba_shares", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.SambaShare]()}
var UpdateNFSShare = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "shares.update_nfs_share", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ShareNFSRequest](), Result: apischema.TypeOf[apischema.SuccessPathResponse]()}
var UpdateSambaShare = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "shares.update_samba_share", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ShareUpdateSambaRequest](), Result: apischema.TypeOf[apischema.SuccessNameResponse]()}

var Routes = []apischema.RouteSpec{
	CreateNFSShare,
	CreateSambaShare,
	DeleteNFSShare,
	DeleteSambaShare,
	ListNFSShares,
	ListSambaShares,
	UpdateNFSShare,
	UpdateSambaShare,
}
