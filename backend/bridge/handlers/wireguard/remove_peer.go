package wireguard

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func RemovePeer(ctx context.Context, req apischema.WireGuardPeerRequest) error {
	if req.InterfaceName == "" || req.PeerID == "" {
		slog.Error("invalid remove peer request")
		return fmt.Errorf("usage: remove_peer <interface> <peerId>")
	}
	if err := validateInterfaceName(req.InterfaceName); err != nil {
		slog.Error("invalid WireGuard interface name", "interface", req.InterfaceName, "error", err)
		return fmt.Errorf("invalid interface name: %w", err)
	}
	if err := validateInterfaceName(req.PeerID); err != nil {
		slog.Error("invalid WireGuard peer id", "peer", req.PeerID, "error", err)
		return fmt.Errorf("invalid peer id: %w", err)
	}
	slog.Info("removing WireGuard peer", "interface", req.InterfaceName, "peer", req.PeerID)

	peerPath := peerConfigPath(req.InterfaceName, req.PeerID)
	file, err := readPeerFile(peerPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("peer %s not found on %s", req.PeerID, req.InterfaceName)
		}
		return err
	}
	publicKey, err := file.publicKey()
	if err != nil {
		return err
	}

	cfg, err := ParseWireGuardConfig(configPath(req.InterfaceName))
	if err != nil {
		slog.Error("failed to read main interface config", "interface", req.InterfaceName, "error", err)
		return fmt.Errorf("read main config: %w", err)
	}
	if replacePeerSection(&cfg, publicKey, nil) {
		if err := WriteWireGuardConfig(configPath(req.InterfaceName), cfg); err != nil {
			slog.Error("failed to write updated interface config", "interface", req.InterfaceName, "error", err)
			return fmt.Errorf("write config: %w", err)
		}
	} else {
		slog.Debug("peer was not present in the interface config", "interface", req.InterfaceName, "peer", req.PeerID)
	}

	if err := os.Remove(peerPath); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to remove peer config file", "interface", req.InterfaceName, "peer", req.PeerID, "path", peerPath, "error", err)
	}

	if err := syncRunningInterface(ctx, req.InterfaceName); err != nil {
		slog.Error("failed to sync running interface after removing peer", "interface", req.InterfaceName, "peer", req.PeerID, "error", err)
		return err
	}

	slog.Info("WireGuard peer removed", "interface", req.InterfaceName, "peer", req.PeerID)
	return nil
}
