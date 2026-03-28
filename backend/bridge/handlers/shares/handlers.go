package shares

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/go-logger/logger"
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
	logger.Debugf("Listing NFS shares")
	shares, err := ListNFSShares()
	if err != nil {
		logger.Errorf("Failed to list NFS shares: %v", err)
		return err
	}
	logger.Debugf("Found %d NFS shares", len(shares))
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

	logger.Infof("Creating NFS share: path=%s clients=%d", path, len(clients))
	if err := CreateNFSShare(path, clients); err != nil {
		logger.Errorf("Failed to create NFS share %s: %v", path, err)
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

	logger.Infof("Updating NFS share: path=%s", path)
	if err := UpdateNFSShare(path, clients); err != nil {
		logger.Errorf("Failed to update NFS share %s: %v", path, err)
		return err
	}
	return emit.Result(map[string]any{"success": true, "path": path})
}

func handleDeleteNFSShare(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		return ipc.ErrInvalidArgs
	}
	path := args[0]
	logger.Infof("Deleting NFS share: path=%s", path)
	if err := DeleteNFSShare(path); err != nil {
		logger.Errorf("Failed to delete NFS share %s: %v", path, err)
		return err
	}
	return emit.Result(map[string]any{"success": true})
}

// --- Samba handlers ---

func handleListSambaShares(ctx context.Context, args []string, emit ipc.Events) error {
	logger.Debugf("Listing Samba shares")
	shares, err := ListSambaShares()
	if err != nil {
		logger.Errorf("Failed to list Samba shares: %v", err)
		return err
	}
	logger.Debugf("Found %d Samba shares", len(shares))
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

	logger.Infof("Creating Samba share: %s", name)
	if err := CreateSambaShare(name, properties); err != nil {
		logger.Errorf("Failed to create Samba share %s: %v", name, err)
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

	logger.Infof("Updating Samba share: %s -> %s", oldName, newName)
	if err := UpdateSambaShare(oldName, newName, properties); err != nil {
		logger.Errorf("Failed to update Samba share %s: %v", oldName, err)
		return err
	}
	return emit.Result(map[string]any{"success": true, "name": newName})
}

func handleDeleteSambaShare(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		return ipc.ErrInvalidArgs
	}
	name := args[0]
	logger.Infof("Deleting Samba share: %s", name)
	if err := DeleteSambaShare(name); err != nil {
		logger.Errorf("Failed to delete Samba share %s: %v", name, err)
		return err
	}
	return emit.Result(map[string]any{"success": true})
}
