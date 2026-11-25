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
	apischema.Mutation("shares.save_rsync", handleSaveRsync, apischema.Privileged()),
	apischema.Mutation("shares.stop_rsync", handleStopRsync, apischema.Privileged()),
	// NFS exports (server-side shares via /etc/exports)
	apischema.Call[apischema.NoRequest, []apischema.NFSExport]("shares.list_nfs_shares", apischema.RetrySafe()).Handle(handleListNFSShares),
	apischema.Mutation("shares.create_nfs_share", handleCreateNFSShare, apischema.Privileged()),
	apischema.Mutation("shares.update_nfs_share", handleUpdateNFSShare, apischema.Privileged()),
	apischema.Mutation("shares.delete_nfs_share", handleDeleteNFSShare, apischema.Privileged()),
	// Samba shares (via /etc/samba/smb.conf)
	apischema.Call[apischema.NoRequest, []apischema.SambaShare]("shares.list_samba_shares", apischema.RetrySafe()).Handle(handleListSambaShares),
	apischema.Mutation("shares.create_samba_share", handleCreateSambaShare, apischema.Privileged()),
	apischema.Mutation("shares.update_samba_share", handleUpdateSambaShare, apischema.Privileged()),
	apischema.Mutation("shares.delete_samba_share", handleDeleteSambaShare, apischema.Privileged()),
	// Shared folders: pool subvolumes with an access table (folders.go). The
	// record is root's alone, so the list is privileged too.
	apischema.Call[apischema.NoRequest, []apischema.SharedFolder]("shares.list_folders", apischema.RetrySafe(), apischema.Privileged()).Handle(handleListSharedFolders),
	apischema.Mutation("shares.create_folder", CreateSharedFolder, apischema.Privileged()),
	apischema.Mutation("shares.update_folder", UpdateSharedFolder, apischema.Privileged()),
	apischema.MutationVoid("shares.delete_folder", DeleteSharedFolder, apischema.Privileged()),
	// Who can sign in to shared folders from Windows and a Mac
	// (samba_users.go). Samba's user list is root's alone.
	apischema.Call[apischema.NoRequest, []apischema.SambaUser]("shares.list_samba_users", apischema.RetrySafe(), apischema.Privileged()).Handle(handleListSambaUsers),
	apischema.MutationVoid("shares.remove_samba_user", handleRemoveSambaUser, apischema.Privileged()),
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

func handleListSharedFolders(ctx context.Context, _ apischema.NoRequest) ([]apischema.SharedFolder, error) {
	return ListSharedFolders(ctx)
}

func handleListSambaUsers(ctx context.Context, _ apischema.NoRequest) ([]apischema.SambaUser, error) {
	return ListSambaUsers(ctx)
}

func handleRemoveSambaUser(ctx context.Context, req apischema.UsernameRequest) error {
	return RemoveSambaUser(ctx, req.Username)
}

func handleCreateNFSShare(ctx context.Context, req apischema.ShareNFSRequest) (apischema.SuccessPathResponse, error) {
	if err := plainShare(ctx, nil, req.Path, true, func() error { return CreateNFSShare(ctx, req.Path, req.Clients) }); err != nil {
		return apischema.SuccessPathResponse{}, err
	}
	return apischema.SuccessPathResponse{Success: true, Path: req.Path}, nil
}

func handleUpdateNFSShare(ctx context.Context, req apischema.ShareNFSRequest) (apischema.SuccessPathResponse, error) {
	if err := plainShare(ctx, nil, req.Path, false, func() error { return UpdateNFSShare(ctx, req.Path, req.Clients) }); err != nil {
		return apischema.SuccessPathResponse{}, err
	}
	return apischema.SuccessPathResponse{Success: true, Path: req.Path}, nil
}

func handleDeleteNFSShare(ctx context.Context, req apischema.PathRequest) (apischema.SuccessResponse, error) {
	if err := plainShare(ctx, nil, req.Path, false, func() error { return DeleteNFSShare(ctx, req.Path) }); err != nil {
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}

func handleListSambaShares(ctx context.Context, _ apischema.NoRequest) ([]apischema.SambaShare, error) {
	return ListSambaShares(ctx)
}

func handleCreateSambaShare(ctx context.Context, req apischema.ShareSambaRequest) (apischema.SuccessNameResponse, error) {
	change := func() error { return CreateSambaShare(ctx, req.Name, req.Properties) }
	if err := plainShare(ctx, []string{req.Name}, req.Properties["path"], true, change); err != nil {
		return apischema.SuccessNameResponse{}, err
	}
	return apischema.SuccessNameResponse{Success: true, Name: req.Name}, nil
}

func handleUpdateSambaShare(ctx context.Context, req apischema.ShareUpdateSambaRequest) (apischema.SuccessNameResponse, error) {
	change := func() error { return UpdateSambaShare(ctx, req.OldName, req.NewName, req.Properties) }
	if err := plainShare(ctx, []string{req.OldName, req.NewName}, req.Properties["path"], false, change); err != nil {
		return apischema.SuccessNameResponse{}, err
	}
	return apischema.SuccessNameResponse{Success: true, Name: req.NewName}, nil
}

func handleDeleteSambaShare(ctx context.Context, req apischema.NameRequest) (apischema.SuccessResponse, error) {
	if err := plainShare(ctx, []string{req.Name}, "", false, func() error { return DeleteSambaShare(ctx, req.Name) }); err != nil {
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}
