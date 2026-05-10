package shares

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers all share management handlers with the global registry
func RegisterHandlers(rt runtime.Runtime) {
	rpc.Register("shares", rt, []rpc.Command{
		// NFS exports (server-side shares via /etc/exports)
		{Name: "list_nfs_shares", Handler: handleListNFSShares},
		{Name: "create_nfs_share", Handler: handleCreateNFSShare},
		{Name: "update_nfs_share", Handler: handleUpdateNFSShare},
		{Name: "delete_nfs_share", Handler: handleDeleteNFSShare},
		// Samba shares (via /etc/samba/smb.conf)
		{Name: "list_samba_shares", Handler: handleListSambaShares},
		{Name: "create_samba_share", Handler: handleCreateSambaShare},
		{Name: "update_samba_share", Handler: handleUpdateSambaShare},
		{Name: "delete_samba_share", Handler: handleDeleteSambaShare},
	})
}

// --- NFS handlers ---

func handleListNFSShares(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Debug("Listing NFS shares")
	shares, err := ListNFSShares()
	if err != nil {
		slog.Error("failed to list NFS shares", "error", err)
		return err
	}
	slog.Debug("listed NFS shares", "count", len(shares))
	return rpc.EmitResult(emit, shares, nil)
}

func handleCreateNFSShare(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 2); err != nil {
		return err
	}
	path := args[0]
	clients, err := rpc.DecodeJSONArg[[]NFSClient](args, 1)
	if err != nil {
		return err
	}
	slog.Info("creating NFS share", "path", path, "count", len(clients))
	if err := CreateNFSShare(path, clients); err != nil {
		slog.Error("failed to create NFS share", "path", path, "error", err)
		return err
	}
	return rpc.EmitResult(emit, map[string]any{"success": true, "path": path}, nil)
}

func handleUpdateNFSShare(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 2); err != nil {
		return err
	}
	path := args[0]
	clients, err := rpc.DecodeJSONArg[[]NFSClient](args, 1)
	if err != nil {
		return err
	}
	slog.Info("updating NFS share", "path", path)
	if err := UpdateNFSShare(path, clients); err != nil {
		slog.Error("failed to update NFS share", "path", path, "error", err)
		return err
	}
	return rpc.EmitResult(emit, map[string]any{"success": true, "path": path}, nil)
}

func handleDeleteNFSShare(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 1); err != nil {
		return err
	}
	path := args[0]
	slog.Info("deleting NFS share", "path", path)
	if err := DeleteNFSShare(path); err != nil {
		slog.Error("failed to delete NFS share", "path", path, "error", err)
		return err
	}
	return rpc.EmitResult(emit, map[string]any{"success": true}, nil)
}

// --- Samba handlers ---

func handleListSambaShares(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Debug("Listing Samba shares")
	shares, err := ListSambaShares()
	if err != nil {
		slog.Error("failed to list Samba shares", "error", err)
		return err
	}
	slog.Debug("listed Samba shares", "count", len(shares))
	return rpc.EmitResult(emit, shares, nil)
}

func handleCreateSambaShare(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 2); err != nil {
		return err
	}
	name := args[0]
	properties, err := rpc.DecodeJSONArg[map[string]string](args, 1)
	if err != nil {
		return err
	}
	slog.Info("creating Samba share", "name", name, "path", properties["path"])
	if err := CreateSambaShare(name, properties); err != nil {
		slog.Error("failed to create Samba share", "name", name, "error", err)
		return err
	}
	return rpc.EmitResult(emit, map[string]any{"success": true, "name": name}, nil)
}

func handleUpdateSambaShare(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 2); err != nil {
		return err
	}
	oldName := args[0]
	newName := oldName
	propertiesArgIndex := 1
	if len(args) >= 3 {
		newName = args[1]
		propertiesArgIndex = 2
	}
	properties, err := rpc.DecodeJSONArg[map[string]string](args, propertiesArgIndex)
	if err != nil {
		return err
	}
	slog.Info("updating Samba share", "name", oldName, "new_name", newName)
	if err := UpdateSambaShare(oldName, newName, properties); err != nil {
		slog.Error("failed to update Samba share", "name", oldName, "error", err)
		return err
	}
	return rpc.EmitResult(emit, map[string]any{"success": true, "name": newName}, nil)
}

func handleDeleteSambaShare(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 1); err != nil {
		return err
	}
	name := args[0]
	slog.Info("deleting Samba share", "name", name)
	if err := DeleteSambaShare(name); err != nil {
		slog.Error("failed to delete Samba share", "name", name, "error", err)
		return err
	}
	return rpc.EmitResult(emit, map[string]any{"success": true}, nil)
}
