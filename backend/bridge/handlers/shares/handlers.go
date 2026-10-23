package shares

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
)

var api = apischema.Bindings(
	apischema.Call[apischema.RsyncSSHRequest, apischema.RsyncSSHStatus]("shares.get_rsync_ssh", apischema.RetrySafe(), apischema.Privileged()).Handle(handleGetRsyncSSH),
	apischema.Call[apischema.NoRequest, apischema.RsyncStatus]("shares.get_rsync", apischema.RetrySafe(), apischema.Privileged()).Handle(handleGetRsync),
	apischema.Call[apischema.RsyncSaveRequest, apischema.RsyncStatus]("shares.save_rsync", apischema.Privileged()).Handle(handleSaveRsync),
	apischema.Call[apischema.NoRequest, apischema.RsyncStatus]("shares.stop_rsync", apischema.Privileged()).Handle(handleStopRsync),
	// NFS exports (server-side shares via /etc/exports)
	apischema.Call[apischema.NoRequest, []apischema.NFSExport]("shares.list_nfs_shares", apischema.RetrySafe()).Handle(handleListNFSShares),
	apischema.Call[apischema.ShareNFSRequest, apischema.SuccessPathResponse]("shares.create_nfs_share").Handle(handleCreateNFSShare),
	apischema.Call[apischema.ShareNFSRequest, apischema.SuccessPathResponse]("shares.update_nfs_share").Handle(handleUpdateNFSShare),
	apischema.Call[apischema.PathRequest, apischema.SuccessResponse]("shares.delete_nfs_share").Handle(handleDeleteNFSShare),
	// Samba shares (via /etc/samba/smb.conf)
	apischema.Call[apischema.NoRequest, []apischema.SambaShare]("shares.list_samba_shares", apischema.RetrySafe()).Handle(handleListSambaShares),
	apischema.Call[apischema.ShareSambaRequest, apischema.SuccessNameResponse]("shares.create_samba_share").Handle(handleCreateSambaShare),
	apischema.Call[apischema.ShareUpdateSambaRequest, apischema.SuccessNameResponse]("shares.update_samba_share").Handle(handleUpdateSambaShare),
	apischema.Call[apischema.NameRequest, apischema.SuccessResponse]("shares.delete_samba_share").Handle(handleDeleteSambaShare),
)

var Routes = api.Routes()

// RegisterHandlers registers all share management handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleGetRsyncSSH(ctx context.Context, req apischema.RsyncSSHRequest) (apischema.RsyncSSHStatus, error) {
	return CheckRsyncSSH(ctx, req.Port)
}

func handleGetRsync(ctx context.Context, _ apischema.NoRequest) (apischema.RsyncStatus, error) {
	return GetRsync(ctx)
}

func handleSaveRsync(ctx context.Context, req apischema.RsyncSaveRequest) (apischema.RsyncStatus, error) {
	return ConfigureRsync(ctx, req)
}

func handleStopRsync(ctx context.Context, _ apischema.NoRequest) (apischema.RsyncStatus, error) {
	return StopRsync(ctx)
}

func handleListNFSShares(ctx context.Context, _ apischema.NoRequest) ([]apischema.NFSExport, error) {
	return ListNFSShares(ctx)
}

func handleCreateNFSShare(ctx context.Context, req apischema.ShareNFSRequest) (apischema.SuccessPathResponse, error) {
	if err := CreateNFSShare(ctx, req.Path, req.Clients); err != nil {
		return apischema.SuccessPathResponse{}, err
	}
	return apischema.SuccessPathResponse{Success: true, Path: req.Path}, nil
}

func handleUpdateNFSShare(ctx context.Context, req apischema.ShareNFSRequest) (apischema.SuccessPathResponse, error) {
	if err := UpdateNFSShare(ctx, req.Path, req.Clients); err != nil {
		return apischema.SuccessPathResponse{}, err
	}
	return apischema.SuccessPathResponse{Success: true, Path: req.Path}, nil
}

func handleDeleteNFSShare(ctx context.Context, req apischema.PathRequest) (apischema.SuccessResponse, error) {
	if err := DeleteNFSShare(ctx, req.Path); err != nil {
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}

func handleListSambaShares(ctx context.Context, _ apischema.NoRequest) ([]apischema.SambaShare, error) {
	return ListSambaShares(ctx)
}

func handleCreateSambaShare(ctx context.Context, req apischema.ShareSambaRequest) (apischema.SuccessNameResponse, error) {
	if err := CreateSambaShare(ctx, req.Name, req.Properties); err != nil {
		return apischema.SuccessNameResponse{}, err
	}
	return apischema.SuccessNameResponse{Success: true, Name: req.Name}, nil
}

func handleUpdateSambaShare(ctx context.Context, req apischema.ShareUpdateSambaRequest) (apischema.SuccessNameResponse, error) {
	if err := UpdateSambaShare(ctx, req.OldName, req.NewName, req.Properties); err != nil {
		return apischema.SuccessNameResponse{}, err
	}
	return apischema.SuccessNameResponse{Success: true, Name: req.NewName}, nil
}

func handleDeleteSambaShare(ctx context.Context, req apischema.NameRequest) (apischema.SuccessResponse, error) {
	if err := DeleteSambaShare(ctx, req.Name); err != nil {
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}
