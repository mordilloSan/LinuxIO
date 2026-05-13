package shares

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers all share management handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	bridgeipc.RegisterRoutes(router, "shares", []bridgeipc.Command{
		// NFS exports (server-side shares via /etc/exports)
		{Name: "list_nfs_shares", Mode: bridgeipc.ModeQuery, Handler: handleListNFSShares},
		{Name: "create_nfs_share", Mode: bridgeipc.ModeJob, Handler: handleCreateNFSShare},
		{Name: "update_nfs_share", Mode: bridgeipc.ModeJob, Handler: handleUpdateNFSShare},
		{Name: "delete_nfs_share", Mode: bridgeipc.ModeJob, Handler: handleDeleteNFSShare},
		// Samba shares (via /etc/samba/smb.conf)
		{Name: "list_samba_shares", Mode: bridgeipc.ModeQuery, Handler: handleListSambaShares},
		{Name: "create_samba_share", Mode: bridgeipc.ModeJob, Handler: handleCreateSambaShare},
		{Name: "update_samba_share", Mode: bridgeipc.ModeJob, Handler: handleUpdateSambaShare},
		{Name: "delete_samba_share", Mode: bridgeipc.ModeJob, Handler: handleDeleteSambaShare},
	})
}

// --- NFS handlers ---

func handleListNFSShares(ctx context.Context, args []string, emit bridgeipc.Events) error {
	slog.Debug("Listing NFS shares")
	shares, err := ListNFSShares(ctx)
	if err != nil {
		slog.Error("failed to list NFS shares", "error", err)
		return err
	}
	slog.Debug("listed NFS shares", "count", len(shares))
	return bridgeipc.EmitResult(emit, shares, nil)
}

func handleCreateNFSShare(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 2); err != nil {
		return err
	}
	path := args[0]
	clients, err := bridgeipc.DecodeJSONArg[[]NFSClient](args, 1)
	if err != nil {
		return err
	}
	slog.Info("creating NFS share", "path", path, "count", len(clients))
	if err := CreateNFSShare(ctx, path, clients); err != nil {
		slog.Error("failed to create NFS share", "path", path, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, map[string]any{"success": true, "path": path}, nil)
}

func handleUpdateNFSShare(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 2); err != nil {
		return err
	}
	path := args[0]
	clients, err := bridgeipc.DecodeJSONArg[[]NFSClient](args, 1)
	if err != nil {
		return err
	}
	slog.Info("updating NFS share", "path", path)
	if err := UpdateNFSShare(ctx, path, clients); err != nil {
		slog.Error("failed to update NFS share", "path", path, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, map[string]any{"success": true, "path": path}, nil)
}

func handleDeleteNFSShare(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 1); err != nil {
		return err
	}
	path := args[0]
	slog.Info("deleting NFS share", "path", path)
	if err := DeleteNFSShare(ctx, path); err != nil {
		slog.Error("failed to delete NFS share", "path", path, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, map[string]any{"success": true}, nil)
}

// --- Samba handlers ---

func handleListSambaShares(ctx context.Context, args []string, emit bridgeipc.Events) error {
	slog.Debug("Listing Samba shares")
	shares, err := ListSambaShares(ctx)
	if err != nil {
		slog.Error("failed to list Samba shares", "error", err)
		return err
	}
	slog.Debug("listed Samba shares", "count", len(shares))
	return bridgeipc.EmitResult(emit, shares, nil)
}

func handleCreateSambaShare(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 2); err != nil {
		return err
	}
	name := args[0]
	properties, err := bridgeipc.DecodeJSONArg[map[string]string](args, 1)
	if err != nil {
		return err
	}
	slog.Info("creating Samba share", "name", name, "path", properties["path"])
	if err := CreateSambaShare(ctx, name, properties); err != nil {
		slog.Error("failed to create Samba share", "name", name, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, map[string]any{"success": true, "name": name}, nil)
}

func handleUpdateSambaShare(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 2); err != nil {
		return err
	}
	oldName := args[0]
	newName := oldName
	propertiesArgIndex := 1
	if len(args) >= 3 {
		newName = args[1]
		propertiesArgIndex = 2
	}
	properties, err := bridgeipc.DecodeJSONArg[map[string]string](args, propertiesArgIndex)
	if err != nil {
		return err
	}
	slog.Info("updating Samba share", "name", oldName, "new_name", newName)
	if err := UpdateSambaShare(ctx, oldName, newName, properties); err != nil {
		slog.Error("failed to update Samba share", "name", oldName, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, map[string]any{"success": true, "name": newName}, nil)
}

func handleDeleteSambaShare(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 1); err != nil {
		return err
	}
	name := args[0]
	slog.Info("deleting Samba share", "name", name)
	if err := DeleteSambaShare(ctx, name); err != nil {
		slog.Error("failed to delete Samba share", "name", name, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, map[string]any{"success": true}, nil)
}
