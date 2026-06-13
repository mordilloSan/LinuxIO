package shares

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	// NFS exports (server-side shares via /etc/exports)
	apischema.Query[apischema.NoRequest, []apischema.NFSExport]("shares.list_nfs_shares").Handle(handleListNFSShares),
	apischema.Job[apischema.ShareNFSRequest, apischema.SuccessPathResponse]("shares.create_nfs_share").Handle(handleCreateNFSShare),
	apischema.Job[apischema.ShareNFSRequest, apischema.SuccessPathResponse]("shares.update_nfs_share").Handle(handleUpdateNFSShare),
	apischema.Job[apischema.PathRequest, apischema.SuccessResponse]("shares.delete_nfs_share").Handle(handleDeleteNFSShare),
	// Samba shares (via /etc/samba/smb.conf)
	apischema.Query[apischema.NoRequest, []apischema.SambaShare]("shares.list_samba_shares").Handle(handleListSambaShares),
	apischema.Job[apischema.ShareSambaRequest, apischema.SuccessNameResponse]("shares.create_samba_share").Handle(handleCreateSambaShare),
	apischema.Job[apischema.ShareUpdateSambaRequest, apischema.SuccessNameResponse]("shares.update_samba_share").Handle(handleUpdateSambaShare),
	apischema.Job[apischema.NameRequest, apischema.SuccessResponse]("shares.delete_samba_share").Handle(handleDeleteSambaShare),
)

var Routes = api.Routes()

// RegisterHandlers registers all share management handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

// --- NFS handlers ---

func handleListNFSShares(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	slog.Debug("Listing NFS shares")
	shares, err := ListNFSShares(ctx)
	if err != nil {
		slog.Error("failed to list NFS shares", "error", err)
		return err
	}
	slog.Debug("listed NFS shares", "count", len(shares))
	return bridgeipc.EmitResult(emit, shares, nil)
}

func handleCreateNFSShare(ctx context.Context, req apischema.ShareNFSRequest, emit bridgeipc.Events) error {
	slog.Info("creating NFS share", "path", req.Path, "count", len(req.Clients))
	if err := CreateNFSShare(ctx, req.Path, req.Clients); err != nil {
		slog.Error("failed to create NFS share", "path", req.Path, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, map[string]any{"success": true, "path": req.Path}, nil)
}

func handleUpdateNFSShare(ctx context.Context, req apischema.ShareNFSRequest, emit bridgeipc.Events) error {
	slog.Info("updating NFS share", "path", req.Path)
	if err := UpdateNFSShare(ctx, req.Path, req.Clients); err != nil {
		slog.Error("failed to update NFS share", "path", req.Path, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, map[string]any{"success": true, "path": req.Path}, nil)
}

func handleDeleteNFSShare(ctx context.Context, req apischema.PathRequest, emit bridgeipc.Events) error {
	slog.Info("deleting NFS share", "path", req.Path)
	if err := DeleteNFSShare(ctx, req.Path); err != nil {
		slog.Error("failed to delete NFS share", "path", req.Path, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, map[string]any{"success": true}, nil)
}

// --- Samba handlers ---

func handleListSambaShares(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	slog.Debug("Listing Samba shares")
	shares, err := ListSambaShares(ctx)
	if err != nil {
		slog.Error("failed to list Samba shares", "error", err)
		return err
	}
	slog.Debug("listed Samba shares", "count", len(shares))
	return bridgeipc.EmitResult(emit, shares, nil)
}

func handleCreateSambaShare(ctx context.Context, req apischema.ShareSambaRequest, emit bridgeipc.Events) error {
	slog.Info("creating Samba share", "name", req.Name, "path", req.Properties["path"])
	if err := CreateSambaShare(ctx, req.Name, req.Properties); err != nil {
		slog.Error("failed to create Samba share", "name", req.Name, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, map[string]any{"success": true, "name": req.Name}, nil)
}

func handleUpdateSambaShare(ctx context.Context, req apischema.ShareUpdateSambaRequest, emit bridgeipc.Events) error {
	slog.Info("updating Samba share", "name", req.OldName, "new_name", req.NewName)
	if err := UpdateSambaShare(ctx, req.OldName, req.NewName, req.Properties); err != nil {
		slog.Error("failed to update Samba share", "name", req.OldName, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, map[string]any{"success": true, "name": req.NewName}, nil)
}

func handleDeleteSambaShare(ctx context.Context, req apischema.NameRequest, emit bridgeipc.Events) error {
	slog.Info("deleting Samba share", "name", req.Name)
	if err := DeleteSambaShare(ctx, req.Name); err != nil {
		slog.Error("failed to delete Samba share", "name", req.Name, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, map[string]any{"success": true}, nil)
}
