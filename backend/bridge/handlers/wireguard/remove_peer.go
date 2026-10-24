package wireguard

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func RemovePeer(ctx context.Context, req apischema.WireGuardPeerRequest) error {
	if req.InterfaceName == "" || req.PeerID == "" {
		return fmt.Errorf("usage: remove_peer <interface> <peerId>")
	}
	if err := validateInterfaceName(req.InterfaceName); err != nil {
		return fmt.Errorf("invalid interface name: %w", err)
	}
	if err := validateInterfaceName(req.PeerID); err != nil {
		return fmt.Errorf("invalid peer id: %w", err)
	}
	slog.Debug("removing WireGuard peer", "interface", req.InterfaceName, "peer", req.PeerID)

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
		return fmt.Errorf("read main config: %w", err)
	}
	if replacePeerSection(&cfg, publicKey, nil) {
		if err := WriteWireGuardConfig(configPath(req.InterfaceName), cfg); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
	} else {
		slog.Debug("peer was not present in the interface config", "interface", req.InterfaceName, "peer", req.PeerID)
	}

	if err := os.Remove(peerPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("failed to remove peer config file", "interface", req.InterfaceName, "peer", req.PeerID, "path", peerPath, "error", err)
	}

	if err := syncRunningInterface(ctx, req.InterfaceName); err != nil {
		return fmt.Errorf("sync %s after removing peer %s: %w", req.InterfaceName, req.PeerID, err)
	}

	slog.Info("WireGuard peer removed", "interface", req.InterfaceName, "peer", req.PeerID)
	return nil
}
