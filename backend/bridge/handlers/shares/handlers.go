package shares

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type sharesRegistration struct {
	command string
	handler ipc.HandlerFunc
}

// RegisterHandlers registers all share management handlers with the global registry
func RegisterHandlers() {
	registerSharesHandlers([]sharesRegistration{
		// NFS exports (server-side shares via /etc/exports)
		{command: "list_nfs_shares", handler: handleListNFSShares},
		{command: "create_nfs_share", handler: handleCreateNFSShare},
		{command: "update_nfs_share", handler: handleUpdateNFSShare},
		{command: "delete_nfs_share", handler: handleDeleteNFSShare},
		// Samba shares (via /etc/samba/smb.conf)
		{command: "list_samba_shares", handler: handleListSambaShares},
		{command: "create_samba_share", handler: handleCreateSambaShare},
		{command: "update_samba_share", handler: handleUpdateSambaShare},
		{command: "delete_samba_share", handler: handleDeleteSambaShare},
	})
}

func registerSharesHandlers(registrations []sharesRegistration) {
	for _, reg := range registrations {
		ipc.RegisterFunc("shares", reg.command, reg.handler)
	}
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
	return emit.Result(shares)
}

func handleCreateNFSShare(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 2 {
		return ipc.ErrInvalidArgs
	}
	path := args[0]
	var clients []NFSClient
	if err := json.Unmarshal([]byte(args[1]), &clients); err != nil {
		return fmt.Errorf("invalid clients JSON: %w", err)
	}
	slog.Info("creating NFS share", "path", path, "count", len(clients))
	if err := CreateNFSShare(path, clients); err != nil {
		slog.Error("failed to create NFS share", "path", path, "error", err)
		return err
	}
	return emit.Result(map[string]any{"success": true, "path": path})
}

func handleUpdateNFSShare(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 2 {
		return ipc.ErrInvalidArgs
	}
	path := args[0]
	var clients []NFSClient
	if err := json.Unmarshal([]byte(args[1]), &clients); err != nil {
		return fmt.Errorf("invalid clients JSON: %w", err)
	}
	slog.Info("updating NFS share", "path", path)
	if err := UpdateNFSShare(path, clients); err != nil {
		slog.Error("failed to update NFS share", "path", path, "error", err)
		return err
	}
	return emit.Result(map[string]any{"success": true, "path": path})
}

func handleDeleteNFSShare(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		return ipc.ErrInvalidArgs
	}
	path := args[0]
	slog.Info("deleting NFS share", "path", path)
	if err := DeleteNFSShare(path); err != nil {
		slog.Error("failed to delete NFS share", "path", path, "error", err)
		return err
	}
	return emit.Result(map[string]any{"success": true})
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
	return emit.Result(shares)
}

func handleCreateSambaShare(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 2 {
		return ipc.ErrInvalidArgs
	}
	name := args[0]
	var properties map[string]string
	if err := json.Unmarshal([]byte(args[1]), &properties); err != nil {
		return fmt.Errorf("invalid properties JSON: %w", err)
	}
	slog.Info("creating Samba share", "name", name, "path", properties["path"])
	if err := CreateSambaShare(name, properties); err != nil {
		slog.Error("failed to create Samba share", "name", name, "error", err)
		return err
	}
	return emit.Result(map[string]any{"success": true, "name": name})
}

func handleUpdateSambaShare(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 2 {
		return ipc.ErrInvalidArgs
	}
	oldName := args[0]
	newName := oldName
	propertiesArgIndex := 1
	if len(args) >= 3 {
		newName = args[1]
		propertiesArgIndex = 2
	}
	var properties map[string]string
	if err := json.Unmarshal([]byte(args[propertiesArgIndex]), &properties); err != nil {
		return fmt.Errorf("invalid properties JSON: %w", err)
	}
	slog.Info("updating Samba share", "name", oldName, "new_name", newName)
	if err := UpdateSambaShare(oldName, newName, properties); err != nil {
		slog.Error("failed to update Samba share", "name", oldName, "error", err)
		return err
	}
	return emit.Result(map[string]any{"success": true, "name": newName})
}

func handleDeleteSambaShare(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		return ipc.ErrInvalidArgs
	}
	name := args[0]
	slog.Info("deleting Samba share", "name", name)
	if err := DeleteSambaShare(name); err != nil {
		slog.Error("failed to delete Samba share", "name", name, "error", err)
		return err
	}
	return emit.Result(map[string]any{"success": true})
}
