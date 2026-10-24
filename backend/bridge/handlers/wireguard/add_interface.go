package wireguard

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

type addInterfaceRequest struct {
	name       string
	addresses  []string
	listenPort int
	egressNic  string
	host       string
	dns        []string
	mtu        int
	numPeers   int
}

func AddInterface(ctx context.Context, request apischema.WireGuardAddInterfaceRequest) (any, error) {
	req, err := parseAddInterfaceRequest(request)
	if err != nil {
		return nil, err
	}

	// Refuse before anything is written: an existing interface would otherwise
	// get a new private key and its peers overwritten while possibly still up.
	if _, statErr := os.Stat(configPath(req.name)); statErr == nil {
		return nil, fmt.Errorf("%w: interface %s already exists", bridgeipc.ErrConflict, req.name)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("check interface config: %w", statErr)
	}

	var peers []PeerConfig
	if req.numPeers > 0 {
		peers, err = generatePeers(req.addresses[0], req.numPeers)
	}
	if err != nil {
		return nil, fmt.Errorf("generate peers for %s: %w", req.name, err)
	}

	privKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("generate private key for %s: %w", req.name, err)
	}

	cfg := buildInterfaceConfig(req, privKey.String(), peers)
	publicIP, gatewayDNS := readInterfaceEndpointInfo(cfg, "AddInterface", req.egressNic)
	if err := exportInterfacePeerConfigs(req.name, peers, cfg, req.addresses[0], publicIP, gatewayDNS); err != nil {
		// No interface config exists yet, so these files would only block a retry.
		_ = os.RemoveAll(peerDirPath(req.name))
		return nil, fmt.Errorf("export peer configs for %s: %w", req.name, err)
	}

	// Write main config, including wg-quick NAT hooks for boot persistence.
	if err := WriteWireGuardConfig(configPath(req.name), cfg); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}

	if err := bringUpInterface(ctx, req.name); err != nil {
		return nil, err
	}
	slog.Info("WireGuard interface created and brought up", "interface", req.name)

	return map[string]any{
		"status":      "created",
		"private_key": privKey.String(),
		"public_key":  privKey.PublicKey().String(),
		"peers":       peers,
	}, nil
}

func parseAddInterfaceRequest(req apischema.WireGuardAddInterfaceRequest) (addInterfaceRequest, error) {
	if req.Name == "" || req.Addresses == "" || req.ListenPort == "" || req.EgressNic == "" {
		return addInterfaceRequest{}, fmt.Errorf("usage: add_interface <name> <addresses> <listenPort> <egressNic> [dns] [mtu] [peers_json] [numPeers]")
	}

	name := req.Name
	if err := validateInterfaceName(name); err != nil {
		return addInterfaceRequest{}, fmt.Errorf("invalid interface name: %w", err)
	}

	validAddresses, err := validateCIDRs(parseCSV(req.Addresses))
	if err != nil {
		return addInterfaceRequest{}, fmt.Errorf("invalid address: %w", err)
	}
	addresses := normalizeAddresses(validAddresses)
	if len(addresses) == 0 {
		return addInterfaceRequest{}, fmt.Errorf("at least one address is required")
	}

	listenPort, err := strconv.Atoi(req.ListenPort)
	if err != nil {
		return addInterfaceRequest{}, fmt.Errorf("invalid listen port: %w", err)
	}
	if listenPort < minListenPort || listenPort > maxListenPort {
		return addInterfaceRequest{}, fmt.Errorf("invalid listen port %d", listenPort)
	}
	if other := interfaceUsingPort(name, listenPort); other != "" {
		return addInterfaceRequest{}, fmt.Errorf("port %d is already used by %s", listenPort, other)
	}

	host := ""
	if req.Host != nil {
		host = strings.TrimSpace(*req.Host)
		if err = validateHost(host); err != nil {
			return addInterfaceRequest{}, err
		}
	}
	dns, err := validateDNS(parseOptionalCSVValue(req.DNS))
	if err != nil {
		return addInterfaceRequest{}, err
	}

	mtu := parseOptionalIntValue(req.MTU, 0)
	if mtu != 0 && (mtu < minMTU || mtu > maxMTU) {
		return addInterfaceRequest{}, fmt.Errorf("invalid MTU %d: use 0 or %d to %d", mtu, minMTU, maxMTU)
	}

	return addInterfaceRequest{
		name:       name,
		addresses:  addresses,
		listenPort: listenPort,
		egressNic:  req.EgressNic,
		host:       host,
		dns:        dns,
		mtu:        mtu,
		numPeers:   parseOptionalIntValue(req.NumPeers, 0),
	}, nil
}

func buildInterfaceConfig(req addInterfaceRequest, privateKey string, peers []PeerConfig) WireGuardConfig {
	cfg := WireGuardConfig{
		PrivateKey: privateKey,
		Address:    req.addresses,
		ListenPort: req.listenPort,
		Host:       req.host,
		DNS:        req.dns,
		MTU:        req.mtu,
		Peers:      peers,
	}
	addNATHooks(&cfg, req.egressNic, req.addresses[0])
	return cfg
}

func readInterfaceEndpointInfo(cfg WireGuardConfig, logPrefix, egressNic string) (string, string) {
	gatewayDNS, err := getGatewayForInterfaceIPv4Func(egressNic)
	if gatewayDNS == "" {
		slog.Debug("no gateway DNS found for interface", "operation", logPrefix, "interface", egressNic, "error", err)
	}
	return resolveEndpointHost(cfg, logPrefix), gatewayDNS
}

func exportInterfacePeerConfigs(name string, peers []PeerConfig, cfg WireGuardConfig, serverAddr, publicIP, gatewayDNS string) error {
	if len(peers) == 0 {
		return nil
	}

	ipMgr, err := newIPManager(serverAddr)
	if err != nil {
		return fmt.Errorf("init IP manager: %w", err)
	}

	for _, peer := range peers {
		if len(peer.AllowedIPs) == 0 {
			slog.Warn("skipping peer export without allowed IPs", "peer", peer.PublicKey)
			continue
		}

		peerOffset := ipMgr.extractHostOffset(peer.AllowedIPs[0])
		if peerOffset < 0 {
			slog.Warn("skipping peer export with invalid allowed IP",
				"peer", peer.PublicKey,
				"path", peer.AllowedIPs[0])
			continue
		}

		id := fmt.Sprintf("Peer%d", peerOffset)
		file, err := buildPeerFile(id, peer.Name, peer, cfg, publicIP, gatewayDNS)
		if err == nil {
			err = exportPeerFile(name, file)
		}
		if err != nil {
			return fmt.Errorf("export config for peer %s: %w", id, err)
		}
	}

	return nil
}

func generatePeers(serverAddr string, count int) ([]PeerConfig, error) {
	ipMgr, err := newIPManager(serverAddr)
	if err != nil {
		return nil, fmt.Errorf("init IP manager: %w", err)
	}

	peers := make([]PeerConfig, 0, count)
	for i := 1; i <= count; i++ {
		peerIP, hostOffset, err := ipMgr.findNextAvailable(peers)
		if err != nil {
			return nil, fmt.Errorf("allocate IP for peer %d: %w", i, err)
		}

		privKey, err := wgtypes.GeneratePrivateKey()
		if err != nil {
			return nil, fmt.Errorf("generate key for peer %d: %w", i, err)
		}

		psk, err := wgtypes.GenerateKey()
		if err != nil {
			return nil, fmt.Errorf("generate preshared key for peer %d: %w", i, err)
		}

		peers = append(peers, PeerConfig{
			PublicKey:           privKey.PublicKey().String(),
			PrivateKey:          privKey.String(),
			PresharedKey:        psk.String(),
			AllowedIPs:          []string{peerIP},
			PersistentKeepalive: defaultKeepalive,
			Name:                fmt.Sprintf("Peer%d", hostOffset),
		})
	}
	slog.Debug("generated peers", "component", "wireguard", "subsystem", "peers", "path", serverAddr, "count", count)
	return peers, nil
}

func parseOptionalCSVValue(raw *string) []string {
	if raw != nil && *raw != "" && *raw != "null" {
		return parseCSV(*raw)
	}
	return nil
}

func parseOptionalIntValue(raw *string, defaultVal int) int {
	if raw == nil {
		return defaultVal
	}
	return parseOptionalIntString(*raw, defaultVal)
}

func parseOptionalIntString(raw string, defaultVal int) int {
	if raw != "" && raw != "null" {
		if val, err := strconv.Atoi(raw); err == nil {
			return val
		}
	}
	return defaultVal
}
