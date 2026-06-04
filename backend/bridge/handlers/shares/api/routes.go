package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var CreateNFSShare = routes.Job("shares.create_nfs_share", apischema.TypeOf[apischema.ShareNFSRequest](), apischema.TypeOf[apischema.SuccessPathResponse]())
var CreateSambaShare = routes.Job("shares.create_samba_share", apischema.TypeOf[apischema.ShareSambaRequest](), apischema.TypeOf[apischema.SuccessNameResponse]())
var DeleteNFSShare = routes.Job("shares.delete_nfs_share", apischema.TypeOf[apischema.PathRequest](), apischema.TypeOf[apischema.SuccessResponse]())
var DeleteSambaShare = routes.Job("shares.delete_samba_share", apischema.TypeOf[apischema.NameRequest](), apischema.TypeOf[apischema.SuccessResponse]())
var ListNFSShares = routes.Query("shares.list_nfs_shares", apischema.NoRequest(), apischema.TypeOf[[]apischema.NFSExport]())
var ListSambaShares = routes.Query("shares.list_samba_shares", apischema.NoRequest(), apischema.TypeOf[[]apischema.SambaShare]())
var UpdateNFSShare = routes.Job("shares.update_nfs_share", apischema.TypeOf[apischema.ShareNFSRequest](), apischema.TypeOf[apischema.SuccessPathResponse]())
var UpdateSambaShare = routes.Job("shares.update_samba_share", apischema.TypeOf[apischema.ShareUpdateSambaRequest](), apischema.TypeOf[apischema.SuccessNameResponse]())

var Routes = routes.All()
