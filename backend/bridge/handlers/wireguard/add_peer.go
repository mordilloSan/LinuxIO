package wireguard

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func AddPeer(ctx context.Context, req apischema.WireGuardAddPeerRequest) (string, error) {
	if req.InterfaceName == "" {
		slog.Error("invalid add peer request")
		return "", fmt.Errorf("usage: add_peer <interface> [name]")
	}

	interfaceName := req.InterfaceName
	if err := validateInterfaceName(interfaceName); err != nil {
		slog.Error("invalid WireGuard interface name", "interface", interfaceName, "error", err)
		return "", fmt.Errorf("invalid interface name: %w", err)
	}
	if err := validatePeerName(req.Name); err != nil {
		return "", err
	}
	slog.Info("adding WireGuard peer", "interface", interfaceName)

	cfg, err := ParseWireGuardConfig(configPath(interfaceName))
	if err != nil {
		slog.Error("failed to read interface config", "interface", interfaceName, "error", err)
		return "", fmt.Errorf("read config: %w", err)
	}
	peer, nextIP, err := createNextPeer(cfg)
	if err != nil {
		slog.Error("failed to create WireGuard peer", "interface", interfaceName, "error", err)
		return "", err
	}

	publicIP, gatewayDNS := readPeerEndpointInfo(cfg, "AddPeer")
	file, err := buildPeerFile(peer.Name, req.Name, peer, cfg, publicIP, gatewayDNS)
	if err != nil {
		slog.Error("failed to build peer config", "interface", interfaceName, "peer", peer.Name, "error", err)
		return "", err
	}
	if err = exportPeerFile(interfaceName, file); err != nil {
		return "", fmt.Errorf("export peer config: %w", err)
	}

	section, err := file.serverSection()
	if err != nil {
		return "", err
	}
	replacePeerSection(&cfg, section.PublicKey, &section)
	if err := WriteWireGuardConfig(configPath(interfaceName), cfg); err != nil {
		if rmErr := os.Remove(peerConfigPath(interfaceName, file.ID)); rmErr != nil {
			slog.Warn("rollback failed while removing peer config", "interface", interfaceName, "peer", file.ID, "error", rmErr)
		}
		slog.Error("failed to write updated interface config", "interface", interfaceName, "error", err)
		return "", fmt.Errorf("write config: %w", err)
	}

	if err := syncRunningInterface(ctx, interfaceName); err != nil {
		slog.Error("failed to sync running interface after adding peer", "interface", interfaceName, "peer", file.ID, "error", err)
		return "", err
	}

	slog.Info("peer added to interface", "interface", interfaceName, "peer", file.ID, "path", nextIP, "running", isInterfaceUpFunc(interfaceName))
	return file.ID, nil
}

// readPeerEndpointInfo resolves what a freshly exported peer needs to reach
// the server: the public IP and, as DNS fallback, the default gateway.
func readPeerEndpointInfo(cfg WireGuardConfig, logPrefix string) (string, string) {
	publicIP := resolveEndpointHost(cfg, logPrefix)
	gatewayDNS, _ := getDefaultGatewayIPv4Func()
	if gatewayDNS == "" {
		slog.Debug("no default gateway DNS found", "operation", logPrefix)
	}
	return publicIP, gatewayDNS
}

func createNextPeer(cfg WireGuardConfig) (PeerConfig, string, error) {
	if len(cfg.Address) == 0 {
		return PeerConfig{}, "", fmt.Errorf("interface has no address")
	}

	ipMgr, err := newIPManager(cfg.Address[0])
	if err != nil {
		return PeerConfig{}, "", fmt.Errorf("init IP manager: %w", err)
	}

	nextIP, peerNumber, err := ipMgr.findNextAvailable(cfg.Peers)
	if err != nil {
		return PeerConfig{}, "", err
	}

	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return PeerConfig{}, "", fmt.Errorf("generate private key: %w", err)
	}
	psk, err := wgtypes.GenerateKey()
	if err != nil {
		return PeerConfig{}, "", fmt.Errorf("generate preshared key: %w", err)
	}

	return PeerConfig{
		PublicKey:           priv.PublicKey().String(),
		PrivateKey:          priv.String(),
		PresharedKey:        psk.String(),
		AllowedIPs:          []string{nextIP},
		PersistentKeepalive: defaultKeepalive,
		Name:                fmt.Sprintf("Peer%d", peerNumber),
	}, nextIP, nil
}
