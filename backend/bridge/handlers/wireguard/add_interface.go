package wireguard

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

type addInterfaceRequest struct {
	name       string
	addresses  []string
	listenPort int
	egressNic  string
	host       string
	dns        []string
	mtu        int
	peers      []PeerConfig
	numPeers   int
}

func AddInterface(ctx context.Context, request apischema.WireGuardAddInterfaceRequest) (any, error) {
	req, err := parseAddInterfaceRequest(request)
	if err != nil {
		slog.Error("invalid add interface arguments", "error", err)
		return nil, err
	}

	peers, err := resolveInterfacePeers(req)
	if err != nil {
		slog.Error("failed to generate interface peers", "interface", req.name, "error", err)
		return nil, fmt.Errorf("generate peers: %w", err)
	}

	privKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		slog.Error("failed to generate WireGuard private key", "interface", req.name, "error", err)
		return nil, fmt.Errorf("generate private key: %w", err)
	}

	cfg := buildInterfaceConfig(req, privKey.String(), peers)
	publicIP, gatewayDNS := readInterfaceEndpointInfo(cfg, "AddInterface", req.egressNic)
	if err := exportInterfacePeerConfigs(req.name, peers, cfg, req.addresses[0], publicIP, gatewayDNS); err != nil {
		slog.Error("failed to export interface peer configs", "interface", req.name, "error", err)
		return nil, err
	}

	// Write main config, including wg-quick NAT hooks for boot persistence.
	if err := WriteWireGuardConfig(configPath(req.name), cfg); err != nil {
		slog.Error("failed to write WireGuard interface config", "interface", req.name, "error", err)
		return nil, fmt.Errorf("write config: %w", err)
	}

	if err := bringUpInterface(ctx, req.name); err != nil {
		slog.Error("failed to set up WireGuard interface", "interface", req.name, "error", err)
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

	addresses := normalizeAddresses(parseCSV(req.Addresses))
	if len(addresses) == 0 {
		return addInterfaceRequest{}, fmt.Errorf("at least one address is required")
	}

	listenPort, err := strconv.Atoi(req.ListenPort)
	if err != nil {
		return addInterfaceRequest{}, fmt.Errorf("invalid listen port: %w", err)
	}

	peers, err := parseOptionalPeersValue(req.PeersJSON)
	if err != nil {
		return addInterfaceRequest{}, fmt.Errorf("invalid peers JSON: %w", err)
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

	return addInterfaceRequest{
		name:       name,
		addresses:  addresses,
		listenPort: listenPort,
		egressNic:  req.EgressNic,
		host:       host,
		dns:        dns,
		mtu:        parseOptionalIntValue(req.MTU, 0),
		peers:      peers,
		numPeers:   parseOptionalIntValue(req.NumPeers, 0),
	}, nil
}

func resolveInterfacePeers(req addInterfaceRequest) ([]PeerConfig, error) {
	if len(req.peers) > 0 || req.numPeers <= 0 {
		return req.peers, nil
	}
	return generatePeers(req.addresses[0], req.numPeers)
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
	gatewayDNS, _ := getGatewayForInterfaceIPv4Func(egressNic)
	if gatewayDNS == "" {
		slog.Debug("no gateway DNS found for interface", "operation", logPrefix, "interface", egressNic)
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
			slog.Warn("failed to export peer config", "interface", name, "peer", peer.PublicKey, "error", err)
		}
	}

	return nil
}

func generatePeers(serverAddr string, count int) ([]PeerConfig, error) {
	ipMgr, err := newIPManager(serverAddr)
	if err != nil {
		slog.Error("failed to initialize peer IP manager", "component", "wireguard", "path", serverAddr, "error", err)
		return nil, err
	}

	peers := make([]PeerConfig, 0, count)
	for i := 1; i <= count; i++ {
		peerIP, hostOffset, err := ipMgr.findNextAvailable(peers)
		if err != nil {
			return nil, fmt.Errorf("allocate IP for peer %d: %w", i, err)
		}

		privKey, err := wgtypes.GeneratePrivateKey()
		if err != nil {
			slog.Error("failed to generate peer key", "component", "wireguard", "subsystem", "peers", "path", serverAddr, "error", err)
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
	slog.Info("generated peers", "component", "wireguard", "subsystem", "peers", "path", serverAddr, "count", count)
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

func parseOptionalPeersValue(raw *string) ([]PeerConfig, error) {
	if raw == nil {
		return nil, nil
	}
	switch *raw {
	case "", "null", "[]":
		return nil, nil
	}
	var peers []PeerConfig
	if err := json.Unmarshal([]byte(*raw), &peers); err != nil {
		return nil, err
	}
	return peers, nil
}
