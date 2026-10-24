package wireguard

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func AddPeer(ctx context.Context, req apischema.WireGuardAddPeerRequest) (string, error) {
	if req.InterfaceName == "" {
		return "", fmt.Errorf("usage: add_peer <interface> [name]")
	}

	interfaceName := req.InterfaceName
	if err := validateInterfaceName(interfaceName); err != nil {
		return "", fmt.Errorf("invalid interface name: %w", err)
	}
	if err := validatePeerName(req.Name); err != nil {
		return "", err
	}
	slog.Debug("adding WireGuard peer", "interface", interfaceName)

	cfg, err := ParseWireGuardConfig(configPath(interfaceName))
	if err != nil {
		return "", fmt.Errorf("read interface config: %w", err)
	}
	// Disabled peers are gone from the server config but keep their exported
	// file and address; their IPs and IDs must stay reserved.
	exported, err := loadExportedPeers(ctx, interfaceName)
	if err != nil {
		return "", err
	}
	peer, nextIP, err := createNextPeer(cfg, exported)
	if err != nil {
		return "", fmt.Errorf("create peer on %s: %w", interfaceName, err)
	}

	publicIP, gatewayDNS := readPeerEndpointInfo(ctx, cfg, "AddPeer")
	file, err := buildPeerFile(peer.Name, req.Name, peer, cfg, publicIP, gatewayDNS)
	if err != nil {
		return "", fmt.Errorf("build config for peer %s: %w", peer.Name, err)
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
		return "", fmt.Errorf("write config: %w", err)
	}

	if err := syncRunningInterface(ctx, interfaceName); err != nil {
		return "", fmt.Errorf("sync %s after adding peer %s: %w", interfaceName, file.ID, err)
	}

	slog.Info("peer added to interface", "interface", interfaceName, "peer", file.ID, "path", nextIP, "running", isInterfaceUpFunc(interfaceName))
	return file.ID, nil
}

// readPeerEndpointInfo resolves what a freshly exported peer needs to reach
// the server: the public IP and, as DNS fallback, the default gateway.
func readPeerEndpointInfo(ctx context.Context, cfg WireGuardConfig, logPrefix string) (string, string) {
	publicIP := resolveEndpointHost(ctx, cfg, logPrefix)
	gatewayDNS, err := getDefaultGatewayIPv4Func()
	if gatewayDNS == "" {
		slog.Debug("no default gateway DNS found", "operation", logPrefix, "error", err)
	}
	return publicIP, gatewayDNS
}

func createNextPeer(cfg WireGuardConfig, exported []PeerInfo) (PeerConfig, string, error) {
	if len(cfg.Address) == 0 {
		return PeerConfig{}, "", fmt.Errorf("interface has no address")
	}

	ipMgr, err := newIPManager(cfg.Address[0])
	if err != nil {
		return PeerConfig{}, "", fmt.Errorf("init IP manager: %w", err)
	}

	used := slices.Clone(cfg.Peers)
	for _, p := range exported {
		used = append(used, PeerConfig{AllowedIPs: []string{p.Address}})
	}
	nextIP, peerNumber, err := ipMgr.findNextAvailable(used)
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
