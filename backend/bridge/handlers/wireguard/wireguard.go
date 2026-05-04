package wireguard

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/skip2/go-qrcode"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gopkg.in/ini.v1"

	"github.com/mordilloSan/LinuxIO/backend/bridge/systemd"
	"github.com/mordilloSan/LinuxIO/backend/bridge/utils"
)

// --- Handler Implementations ---
func ListInterfaces([]string) (any, error) {
	slog.Debug("ListInterfaces: listing interfaces")

	pattern := filepath.Join(wgConfigDir, "*"+configExt)
	files, err := filepath.Glob(pattern)
	if err != nil {
		slog.Error("failed to list WireGuard interface configs", "path", pattern, "error", err)
		return nil, fmt.Errorf("list interfaces: %w", err)
	}

	interfaces := make([]WireGuardInterfaceUI, 0, len(files))

	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), configExt)
		cfg, err := ParseWireGuardConfig(f)
		if err != nil {
			slog.Warn("failed to parse WireGuard interface config", "interface", name, "path", f, "error", err)
			continue
		}

		status := "Inactive"
		if isInterfaceUp(name) {
			status = "Active"
		}

		interfaces = append(interfaces, WireGuardInterfaceUI{
			Name:        name,
			IsConnected: status,
			Address:     strings.Join(cfg.Address, ", "),
			Port:        cfg.ListenPort,
			PeerCount:   len(cfg.Peers),
			IsEnabled:   isInterfaceEnabled(name),
		})
	}
	slog.Info("listed WireGuard interfaces", "count", len(interfaces))
	return interfaces, nil
}

type addInterfaceRequest struct {
	name       string
	addresses  []string
	listenPort int
	egressNic  string
	dns        []string
	mtu        int
	peers      []PeerConfig
	numPeers   int
}

type peerRuntimeStats struct {
	LastHandshake     string
	LastHandshakeUnix int64
	RxBytes           int64
	TxBytes           int64
	RxBps             float64
	TxBps             float64
}

func parseAddInterfaceArgs(args []string) (addInterfaceRequest, error) {
	if len(args) < 4 {
		return addInterfaceRequest{}, fmt.Errorf("usage: add_interface <name> <addresses> <listenPort> <egressNic> [dns] [mtu] [peers_json] [numPeers]")
	}

	name := args[0]
	if err := validateInterfaceName(name); err != nil {
		return addInterfaceRequest{}, fmt.Errorf("invalid interface name: %w", err)
	}

	addresses := normalizeAddresses(parseCSV(args[1]))
	if len(addresses) == 0 {
		return addInterfaceRequest{}, fmt.Errorf("at least one address is required")
	}

	listenPort, err := strconv.Atoi(args[2])
	if err != nil {
		return addInterfaceRequest{}, fmt.Errorf("invalid listen port: %w", err)
	}

	return addInterfaceRequest{
		name:       name,
		addresses:  addresses,
		listenPort: listenPort,
		egressNic:  args[3],
		dns:        parseOptionalCSV(args, 4),
		mtu:        parseOptionalInt(args, 5, 0),
		peers:      parseOptionalPeers(args, 6),
		numPeers:   parseOptionalInt(args, 7, 0),
	}, nil
}

func resolveInterfacePeers(req addInterfaceRequest) ([]PeerConfig, error) {
	if len(req.peers) > 0 || req.numPeers <= 0 {
		return req.peers, nil
	}
	return generatePeers(req.addresses[0], req.numPeers)
}

func buildInterfaceConfig(req addInterfaceRequest, privateKey string, peers []PeerConfig) InterfaceConfig {
	return InterfaceConfig{
		PrivateKey: privateKey,
		Address:    req.addresses,
		ListenPort: req.listenPort,
		DNS:        req.dns,
		MTU:        req.mtu,
		Peers:      peers,
	}
}

func readInterfaceEndpointInfo(logPrefix, egressNic string) (string, string) {
	publicIP, _ := utils.GetPublicIP()
	if publicIP == "" {
		slog.Warn("public IP lookup returned empty string", "operation", logPrefix)
	}

	gatewayDNS, _ := getGatewayForInterfaceIPv4(egressNic)
	if gatewayDNS == "" {
		slog.Debug("no gateway DNS found for interface", "operation", logPrefix, "interface", egressNic)
	}
	return publicIP, gatewayDNS
}

func exportInterfacePeerConfigs(name string, peers []PeerConfig, cfg InterfaceConfig, serverAddr, publicIP, gatewayDNS string) error {
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

		if _, err := ExportPeerConfig(name, peer, cfg, publicIP, peerOffset, gatewayDNS); err != nil {
			slog.Warn("failed to export peer config", "interface", name, "peer", peer.PublicKey, "error", err)
		}
	}

	return nil
}

func bringUpInterfaceWithNAT(name, egressNic, subnet string) error {
	if _, err := UpInterface([]string{name}); err != nil {
		return err
	}

	backendName, err := SetupNAT(name, egressNic, subnet)
	if err != nil {
		if _, downErr := DownInterface([]string{name}); downErr != nil {
			slog.Warn("failed to bring down interface after NAT failure",
				"interface", name,
				"error", downErr)
		}
		return fmt.Errorf("setup NAT: %w", err)
	}

	if err := SaveNATConfig(name, egressNic, subnet, backendName); err != nil {
		slog.Warn("failed to save NAT config", "interface", name, "error", err)
	}

	return nil
}

func createNextPeer(cfg InterfaceConfig) (PeerConfig, string, error) {
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

	return PeerConfig{
		PublicKey:           priv.PublicKey().String(),
		PrivateKey:          priv.String(),
		AllowedIPs:          []string{nextIP},
		PersistentKeepalive: defaultKeepalive,
		Name:                fmt.Sprintf("Peer%d", peerNumber),
	}, nextIP, nil
}

func exportAddedPeer(interfaceName string, peer PeerConfig, cfg InterfaceConfig) error {
	publicIP, _ := utils.GetPublicIP()
	if publicIP == "" {
		slog.Warn("AddPeer: GetPublicIP returned empty string")
	}

	gatewayDNS, _ := getDefaultGatewayIPv4()
	if gatewayDNS == "" {
		slog.Debug("AddPeer: no default gateway DNS found (optional - will use interface DNS if configured)")
	}

	cfgWithPeer := cfg
	cfgWithPeer.Peers = append(cfgWithPeer.Peers, peer)
	peerNumber := parseOptionalInt([]string{peer.Name[4:]}, 0, 0)
	_, err := ExportPeerConfig(interfaceName, peer, cfgWithPeer, publicIP, peerNumber, gatewayDNS)
	if err != nil {
		return fmt.Errorf("export peer config: %w", err)
	}
	return nil
}

func writePeerConfig(interfaceName string, cfg InterfaceConfig, peer PeerConfig) error {
	cfg.Peers = append(cfg.Peers, peer)
	if err := WriteWireGuardConfig(configPath(interfaceName), cfg); err != nil {
		if rmErr := os.Remove(peerConfigPath(interfaceName, peer.Name)); rmErr != nil {
			slog.Warn("rollback failed while removing peer config",
				"interface", interfaceName,
				"peer", peer.Name,
				"error", rmErr)
		}
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func applyPeerToRunningInterface(interfaceName string, peer PeerConfig) error {
	if !isInterfaceUp(interfaceName) {
		return nil
	}

	pubKey, err := wgtypes.ParseKey(peer.PublicKey)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}

	allowedIPs := make([]net.IPNet, 0, len(peer.AllowedIPs))
	for _, ipStr := range peer.AllowedIPs {
		_, ipNet, parseErr := net.ParseCIDR(ipStr)
		if parseErr != nil {
			slog.Warn("failed to parse peer allowed IP", "interface", interfaceName, "path", ipStr, "error", parseErr)
			continue
		}
		allowedIPs = append(allowedIPs, *ipNet)
	}

	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("create wgctrl client: %w", err)
	}
	defer client.Close()

	keepalive := time.Duration(peer.PersistentKeepalive) * time.Second
	peerCfg := wgtypes.PeerConfig{
		PublicKey:                   pubKey,
		AllowedIPs:                  allowedIPs,
		ReplaceAllowedIPs:           true,
		PersistentKeepaliveInterval: &keepalive,
	}

	if err := client.ConfigureDevice(interfaceName, wgtypes.Config{
		Peers: []wgtypes.PeerConfig{peerCfg},
	}); err != nil {
		return fmt.Errorf("configure device: %w", err)
	}
	return nil
}

func readPeerAllowedIP(interfaceName, peerName string) (string, string, error) {
	peerPath := peerConfigPath(interfaceName, peerName)
	iniFile, err := ini.Load(peerPath)
	if err != nil {
		return "", "", fmt.Errorf("parse peer config: %w", err)
	}

	allowedIP := iniFile.Section("Interface").Key("Address").String()
	if allowedIP == "" {
		return "", "", fmt.Errorf("peer config missing Address")
	}

	return peerPath, allowedIP, nil
}

func removePeerFromConfig(cfg InterfaceConfig, allowedIP string) (InterfaceConfig, string, bool) {
	newPeers := make([]PeerConfig, 0, len(cfg.Peers))
	var removedPeerPubKey string
	found := false

	for _, peer := range cfg.Peers {
		if slices.Contains(peer.AllowedIPs, allowedIP) {
			found = true
			removedPeerPubKey = peer.PublicKey
			continue
		}
		newPeers = append(newPeers, peer)
	}

	cfg.Peers = newPeers
	return cfg, removedPeerPubKey, found
}

func removePeerFromRunningInterface(interfaceName, removedPeerPubKey string) {
	if !isInterfaceUp(interfaceName) || removedPeerPubKey == "" {
		return
	}

	pubKey, err := wgtypes.ParseKey(removedPeerPubKey)
	if err != nil {
		slog.Warn("failed to parse removed peer public key", "interface", interfaceName, "error", err)
		return
	}

	client, err := wgctrl.New()
	if err != nil {
		slog.Warn("failed to create wgctrl client for peer removal", "interface", interfaceName, "error", err)
		return
	}
	defer client.Close()

	peerCfg := wgtypes.PeerConfig{
		PublicKey: pubKey,
		Remove:    true,
	}
	if err := client.ConfigureDevice(interfaceName, wgtypes.Config{
		Peers: []wgtypes.PeerConfig{peerCfg},
	}); err != nil {
		slog.Warn("failed to remove peer from running interface", "interface", interfaceName, "error", err)
		return
	}
	slog.Info("removed peer from running interface", "interface", interfaceName)
}

func loadExportedPeers(interfaceName string) ([]PeerInfo, error) {
	pattern := filepath.Join(peerDirPath(interfaceName), "*"+configExt)
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("list peer configs: %w", err)
	}

	peers := make([]PeerInfo, 0, len(files))
	for _, file := range files {
		peer, err := loadExportedPeer(file)
		if err != nil {
			slog.Warn("failed to parse exported peer config", "path", file, "error", err)
			continue
		}
		peers = append(peers, peer)
	}

	return peers, nil
}

func loadExportedPeer(file string) (PeerInfo, error) {
	iniFile, err := ini.Load(file)
	if err != nil {
		return PeerInfo{}, err
	}

	ifSec := iniFile.Section("Interface")
	peerSec := iniFile.Section("Peer")
	pc := PeerConfig{
		PrivateKey:          ifSec.Key("PrivateKey").String(),
		AllowedIPs:          parseCSV(ifSec.Key("Address").String()),
		PublicKey:           peerSec.Key("PublicKey").String(),
		PresharedKey:        peerSec.Key("PresharedKey").String(),
		Endpoint:            peerSec.Key("Endpoint").String(),
		Name:                ifSec.Key("Name").String(),
		PersistentKeepalive: peerSec.Key("PersistentKeepalive").MustInt(0),
	}

	if pc.Name == "" {
		pc.Name = strings.TrimSuffix(filepath.Base(file), configExt)
	}

	return PeerInfo{
		PeerConfig:        pc,
		LastHandshake:     "never",
		LastHandshakeUnix: 0,
		RxBytes:           0,
		TxBytes:           0,
		RxBps:             0,
		TxBps:             0,
	}, nil
}

func mergeConfiguredPeerPublicKeys(interfaceName string, peers []PeerInfo) {
	cfg, err := ParseWireGuardConfig(configPath(interfaceName))
	if err != nil {
		slog.Warn("could not parse main interface config to map peer keys", "interface", interfaceName, "error", err)
		return
	}

	ipToPub := make(map[string]string, len(cfg.Peers))
	for _, peer := range cfg.Peers {
		for _, ip := range peer.AllowedIPs {
			ipToPub[ip] = peer.PublicKey
		}
	}

	for i := range peers {
		if len(peers[i].AllowedIPs) == 0 {
			continue
		}
		if pub, ok := ipToPub[peers[i].AllowedIPs[0]]; ok && pub != "" {
			peers[i].PublicKey = pub
		}
	}
}

func loadPeerRuntimeStats(interfaceName string) (map[string]peerRuntimeStats, error) {
	client, err := wgctrl.New()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	dev, err := client.Device(interfaceName)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	stats := make(map[string]peerRuntimeStats, len(dev.Peers))
	for _, peer := range dev.Peers {
		pub := peer.PublicKey.String()
		rxBps, txBps := computeRates(interfaceName, pub, peer.ReceiveBytes, peer.TransmitBytes, now)
		stats[pub] = peerRuntimeStats{
			LastHandshake:     formatPeerHandshake(peer.LastHandshakeTime),
			LastHandshakeUnix: peer.LastHandshakeTime.Unix(),
			RxBytes:           peer.ReceiveBytes,
			TxBytes:           peer.TransmitBytes,
			RxBps:             rxBps,
			TxBps:             txBps,
		}
		if peer.LastHandshakeTime.IsZero() {
			stats[pub] = peerRuntimeStats{
				LastHandshake:     "never",
				LastHandshakeUnix: 0,
				RxBytes:           peer.ReceiveBytes,
				TxBytes:           peer.TransmitBytes,
				RxBps:             rxBps,
				TxBps:             txBps,
			}
		}
	}

	return stats, nil
}

func formatPeerHandshake(ts time.Time) string {
	if ts.IsZero() {
		return "never"
	}
	return ts.UTC().Format(time.RFC3339)
}

func applyPeerRuntimeStats(peers []PeerInfo, statsByPub map[string]peerRuntimeStats) {
	for i := range peers {
		stats, ok := statsByPub[peers[i].PublicKey]
		if !ok {
			continue
		}
		peers[i].LastHandshake = stats.LastHandshake
		peers[i].LastHandshakeUnix = stats.LastHandshakeUnix
		peers[i].RxBytes = stats.RxBytes
		peers[i].TxBytes = stats.TxBytes
		peers[i].RxBps = stats.RxBps
		peers[i].TxBps = stats.TxBps
	}
}

func AddInterface(args []string) (any, error) {
	req, err := parseAddInterfaceArgs(args)
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
	publicIP, gatewayDNS := readInterfaceEndpointInfo("AddInterface", req.egressNic)
	if err := exportInterfacePeerConfigs(req.name, peers, cfg, req.addresses[0], publicIP, gatewayDNS); err != nil {
		slog.Error("failed to export interface peer configs", "interface", req.name, "error", err)
		return nil, err
	}

	// Write main config (without PostUp/PostDown - we manage NAT programmatically)
	if err := WriteWireGuardConfig(configPath(req.name), cfg); err != nil {
		slog.Error("failed to write WireGuard interface config", "interface", req.name, "error", err)
		return nil, fmt.Errorf("write config: %w", err)
	}

	subnet := req.addresses[0]
	if err := bringUpInterfaceWithNAT(req.name, req.egressNic, subnet); err != nil {
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

func RemoveInterface(args []string) (any, error) {
	if len(args) < 1 {
		slog.Error("invalid remove interface arguments", "args", args)
		return nil, fmt.Errorf("usage: remove_interface <name>")
	}

	name := args[0]
	if err := validateInterfaceName(name); err != nil {
		slog.Error("invalid WireGuard interface name", "interface", name, "error", err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	slog.Info("removing WireGuard interface", "interface", name)

	// Cleanup NAT rules before bringing down the interface
	if natCfg, err := LoadNATConfig(name); err != nil {
		slog.Warn("failed to load NAT config", "interface", name, "error", err)
	} else if natCfg != nil {
		if err := CleanupNAT(name, natCfg.EgressNic, natCfg.Subnet, natCfg.Backend); err != nil {
			slog.Warn("failed to clean up NAT", "interface", name, "error", err)
		}
		if err := RemoveNATConfig(name); err != nil {
			slog.Warn("failed to remove NAT config", "interface", name, "error", err)
		}
	}

	// Best-effort: bring it down, but don't abort on failure.
	if _, err := DownInterface([]string{name}); err != nil {
		slog.Warn("failed to bring down interface before removal", "interface", name, "error", err)
	} else {
		slog.Info("interface brought down before removal", "interface", name)
	}

	// Remove config file
	cfgPath := configPath(name)
	if err := os.Remove(cfgPath); err != nil {
		if os.IsNotExist(err) {
			slog.Warn("interface config already missing", "interface", name, "path", cfgPath)
		} else {
			slog.Error("failed to remove interface config", "interface", name, "path", cfgPath, "error", err)
			return nil, fmt.Errorf("remove config: %w", err)
		}
	} else {
		slog.Info("removed interface config file", "interface", name, "path", cfgPath)
	}

	// Remove peer directory (best-effort)
	peerDir := peerDirPath(name)
	if err := os.RemoveAll(peerDir); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to remove peer directory", "interface", name, "path", peerDir, "error", err)
	} else {
		slog.Info("removed peer directory", "interface", name, "path", peerDir)
	}
	slog.Info("WireGuard interface removed", "interface", name)
	return "removed", nil
}

func AddPeer(args []string) (any, error) {
	if len(args) < 1 {
		slog.Error("invalid add peer arguments", "args", args)
		return nil, fmt.Errorf("usage: add_peer <interface>")
	}

	interfaceName := args[0]
	if err := validateInterfaceName(interfaceName); err != nil {
		slog.Error("invalid WireGuard interface name", "interface", interfaceName, "error", err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	slog.Info("adding WireGuard peer", "interface", interfaceName)

	// Read current config
	cfg, err := ParseWireGuardConfig(configPath(interfaceName))
	if err != nil {
		slog.Error("failed to read interface config", "interface", interfaceName, "error", err)
		return nil, fmt.Errorf("read config: %w", err)
	}

	peer, nextIP, err := createNextPeer(cfg)
	if err != nil {
		slog.Error("failed to create WireGuard peer", "interface", interfaceName, "error", err)
		return nil, err
	}

	if err := exportAddedPeer(interfaceName, peer, cfg); err != nil {
		slog.Error("failed to export added peer config", "interface", interfaceName, "peer", peer.Name, "error", err)
		return nil, err
	}

	if err := writePeerConfig(interfaceName, cfg, peer); err != nil {
		slog.Error("failed to write updated interface config", "interface", interfaceName, "error", err)
		return nil, err
	}

	if err := applyPeerToRunningInterface(interfaceName, peer); err != nil {
		slog.Error("failed to apply peer to running interface", "interface", interfaceName, "peer", peer.Name, "error", err)
		return nil, err
	}

	if isInterfaceUp(interfaceName) {
		slog.Info("peer dynamically added to running interface", "interface", interfaceName, "peer", peer.Name, "path", nextIP)
	} else {
		slog.Info("peer added to interface config", "interface", interfaceName, "peer", peer.Name, "path", nextIP)
	}
	return map[string]any{
		"peer_name":  peer.Name,
		"public_key": peer.PublicKey,
		"allowed_ip": nextIP,
	}, nil
}

func RemovePeerByName(args []string) (any, error) {
	if len(args) < 2 {
		slog.Error("invalid remove peer arguments", "args", args)
		return nil, fmt.Errorf("usage: remove_peer <interface> <peer>")
	}

	interfaceName := args[0]
	peerName := args[1]
	if err := validateInterfaceName(interfaceName); err != nil {
		slog.Error("invalid WireGuard interface name", "interface", interfaceName, "error", err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	if err := validateInterfaceName(peerName); err != nil {
		slog.Error("invalid WireGuard peer name", "peer", peerName, "error", err)
		return nil, fmt.Errorf("invalid peer name: %w", err)
	}
	slog.Info("removing WireGuard peer", "interface", interfaceName, "peer", peerName)

	peerPath, allowedIP, err := readPeerAllowedIP(interfaceName, peerName)
	if err != nil {
		slog.Error("failed to read peer config", "interface", interfaceName, "peer", peerName, "path", peerPath, "error", err)
		return nil, err
	}

	// Read main config
	cfg, err := ParseWireGuardConfig(configPath(interfaceName))
	if err != nil {
		slog.Error("failed to read main interface config", "interface", interfaceName, "error", err)
		return nil, fmt.Errorf("read main config: %w", err)
	}

	cfg, removedPeerPubKey, found := removePeerFromConfig(cfg, allowedIP)
	if !found {
		slog.Error("peer allowed IP not found in interface config", "interface", interfaceName, "path", allowedIP)
		return nil, fmt.Errorf("peer not found with IP %s", allowedIP)
	}

	// Write updated config
	if err := WriteWireGuardConfig(configPath(interfaceName), cfg); err != nil {
		slog.Error("failed to write updated interface config", "interface", interfaceName, "error", err)
		return nil, fmt.Errorf("write config: %w", err)
	}

	removePeerFromRunningInterface(interfaceName, removedPeerPubKey)

	// Remove peer config file
	if err := os.Remove(peerPath); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to remove peer config file", "interface", interfaceName, "peer", peerName, "path", peerPath, "error", err)
	} else {
		slog.Info("removed peer config file", "interface", interfaceName, "peer", peerName, "path", peerPath)
	}
	slog.Info("WireGuard peer removed", "interface", interfaceName, "peer", peerName)
	return "removed", nil
}

func ListPeers(args []string) (any, error) {
	if len(args) < 1 {
		slog.Error("invalid list peers arguments", "args", args)
		return nil, fmt.Errorf("usage: list_exported_peers <interface>")
	}

	interfaceName := args[0]
	if err := validateInterfaceName(interfaceName); err != nil {
		slog.Error("invalid WireGuard interface name", "interface", interfaceName, "error", err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	slog.Debug("listing exported peers", "interface", interfaceName)

	peers, err := loadExportedPeers(interfaceName)
	if err != nil {
		slog.Error("failed to read exported peers", "interface", interfaceName, "error", err)
		return nil, err
	}

	mergeConfiguredPeerPublicKeys(interfaceName, peers)

	statsByPub, err := loadPeerRuntimeStats(interfaceName)
	if err != nil {
		slog.Warn("failed to load runtime peer stats", "interface", interfaceName, "error", err)
		slog.Info("listed exported peers", "interface", interfaceName, "count", len(peers))
		return peers, nil
	}
	applyPeerRuntimeStats(peers, statsByPub)
	slog.Info("listed exported peers with runtime stats", "interface", interfaceName, "count", len(peers))
	return peers, nil
}

func UpInterface(args []string) (any, error) {
	if len(args) < 1 {
		slog.Error("invalid up interface arguments", "args", args)
		return nil, fmt.Errorf("usage: up_interface <name>")
	}
	name := args[0]
	if err := validateInterfaceName(name); err != nil {
		slog.Error("invalid WireGuard interface name", "interface", name, "error", err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	wgQuickPath, err := exec.LookPath("wg-quick")
	if err != nil {
		return nil, fmt.Errorf("wg-quick not found: %w", err)
	}
	cmd := exec.Command(wgQuickPath, "up", name)

	// Ensure real/effective/saved IDs are 0 in the child
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: 0, Gid: 0},
	}

	// predictable env
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	cmd.Dir = "/"

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("failed to bring up WireGuard interface", "interface", name, "error", err, "output", string(out))
		return nil, fmt.Errorf("bring up interface: %w", err)
	}
	slog.Info("WireGuard interface brought up", "interface", name)
	return map[string]any{
		"status": "on",
		"output": string(out),
	}, nil
}

func DownInterface(args []string) (any, error) {
	if len(args) < 1 {
		slog.Error("invalid down interface arguments", "args", args)
		return nil, fmt.Errorf("usage: down_interface <name>")
	}

	name := args[0]
	if err := validateInterfaceName(name); err != nil {
		slog.Error("invalid WireGuard interface name", "interface", name, "error", err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	wgQuickPath, err := exec.LookPath("wg-quick")
	if err != nil {
		return nil, fmt.Errorf("wg-quick not found: %w", err)
	}
	cmd := exec.Command(wgQuickPath, "down", name)

	// Ensure real/effective/saved IDs are 0 in the child
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: 0, Gid: 0},
	}

	// predictable env
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	cmd.Dir = "/"

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("failed to bring down WireGuard interface", "interface", name, "error", err, "output", string(out))
		return nil, fmt.Errorf("bring down interface: %w", err)
	}
	slog.Info("WireGuard interface brought down", "interface", name)
	return map[string]any{
		"status": "off",
		"output": string(out),
	}, nil
}

func PeerQRCode(args []string) (any, error) {
	if len(args) < 2 {
		slog.Error("invalid peer QR code arguments", "args", args)
		return nil, fmt.Errorf("usage: peer_qrcode <interface> <peername>")
	}

	if err := validateInterfaceName(args[0]); err != nil {
		slog.Error("invalid WireGuard interface name", "interface", args[0], "error", err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	if err := validateInterfaceName(args[1]); err != nil {
		slog.Error("invalid WireGuard peer name", "peer", args[1], "error", err)
		return nil, fmt.Errorf("invalid peer name: %w", err)
	}

	peerPath := peerConfigPath(args[0], args[1])
	rawConfig, err := os.ReadFile(peerPath)
	if err != nil {
		slog.Error("failed to read peer config for QR code", "path", peerPath, "error", err)
		return nil, fmt.Errorf("read peer config: %w", err)
	}

	// Generate QR code
	png, err := qrcode.Encode(string(rawConfig), qrcode.Medium, 256)
	if err != nil {
		slog.Error("failed to generate peer QR code", "path", peerPath, "error", err)
		return nil, fmt.Errorf("generate QR code: %w", err)
	}

	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	slog.Info("generated peer QR code", "path", peerPath)
	return map[string]string{"qrcode": dataURI}, nil
}

func PeerConfigDownload(args []string) (any, error) {
	if len(args) < 2 {
		slog.Error("invalid peer config download arguments", "args", args)
		return nil, fmt.Errorf("usage: peer_config_download <interface> <peername>")
	}

	if err := validateInterfaceName(args[0]); err != nil {
		slog.Error("invalid WireGuard interface name", "interface", args[0], "error", err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	if err := validateInterfaceName(args[1]); err != nil {
		slog.Error("invalid WireGuard peer name", "peer", args[1], "error", err)
		return nil, fmt.Errorf("invalid peer name: %w", err)
	}

	peerPath := peerConfigPath(args[0], args[1])
	data, err := os.ReadFile(peerPath)
	if err != nil {
		slog.Error("failed to read peer config for download", "path", peerPath, "error", err)
		return nil, fmt.Errorf("read peer config: %w", err)
	}
	slog.Info("served peer config download", "path", peerPath, "size", len(data))
	return map[string]string{"config": string(data)}, nil
}

func GetKeys(args []string) (any, error) {
	if len(args) < 1 {
		slog.Error("invalid get keys arguments", "args", args)
		return nil, fmt.Errorf("usage: get_keys <interface>")
	}

	if err := validateInterfaceName(args[0]); err != nil {
		slog.Error("invalid WireGuard interface name", "interface", args[0], "error", err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}

	cfg, err := ParseWireGuardConfig(configPath(args[0]))
	if err != nil {
		slog.Error("failed to read interface config for keys", "interface", args[0], "error", err)
		return nil, fmt.Errorf("read config: %w", err)
	}

	key, err := wgtypes.ParseKey(cfg.PrivateKey)
	if err != nil {
		slog.Error("failed to parse WireGuard private key", "interface", args[0], "error", err)
		return nil, fmt.Errorf("parse key: %w", err)
	}
	slog.Info("provided interface keys", "interface", args[0])
	return map[string]string{
		"private_key": cfg.PrivateKey,
		"public_key":  key.PublicKey().String(),
	}, nil
}

func EnableInterface(args []string) (any, error) {
	if len(args) < 1 {
		slog.Error("invalid enable interface arguments", "args", args)
		return nil, fmt.Errorf("usage: enable_interface <name>")
	}

	name := args[0]
	if err := validateInterfaceName(name); err != nil {
		slog.Error("invalid WireGuard interface name", "interface", name, "error", err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}

	serviceName := fmt.Sprintf("wg-quick@%s.service", name)
	if err := systemd.EnableUnit(serviceName); err != nil {
		slog.Error("failed to enable WireGuard interface", "interface", name, "service", serviceName, "error", err)
		return nil, fmt.Errorf("enable interface: %w", err)
	}
	slog.Info("enabled WireGuard interface for boot persistence", "interface", name, "service", serviceName)
	return map[string]any{
		"status": "enabled",
		"output": "enabled via systemd D-Bus",
	}, nil
}

func DisableInterface(args []string) (any, error) {
	if len(args) < 1 {
		slog.Error("invalid disable interface arguments", "args", args)
		return nil, fmt.Errorf("usage: disable_interface <name>")
	}

	name := args[0]
	if err := validateInterfaceName(name); err != nil {
		slog.Error("invalid WireGuard interface name", "interface", name, "error", err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}

	serviceName := fmt.Sprintf("wg-quick@%s.service", name)
	if err := systemd.DisableUnit(serviceName); err != nil {
		slog.Error("failed to disable WireGuard interface", "interface", name, "service", serviceName, "error", err)
		return nil, fmt.Errorf("disable interface: %w", err)
	}
	slog.Info("disabled WireGuard interface from boot persistence", "interface", name, "service", serviceName)
	return map[string]any{
		"status": "disabled",
		"output": "disabled via systemd D-Bus",
	}, nil
}
