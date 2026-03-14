package wireguard

import (
	"encoding/base64"
	"fmt"
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
	"github.com/mordilloSan/go-logger/logger"
)

// --- Handler Implementations ---
func ListInterfaces([]string) (any, error) {
	logger.Debugf("ListInterfaces: listing interfaces")

	pattern := filepath.Join(wgConfigDir, "*"+configExt)
	files, err := filepath.Glob(pattern)
	if err != nil {
		logger.Errorf("ListInterfaces: glob failed for %s: %v", pattern, err)
		return nil, fmt.Errorf("list interfaces: %w", err)
	}

	interfaces := make([]WireGuardInterfaceUI, 0, len(files))

	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), configExt)
		cfg, err := ParseWireGuardConfig(f)
		if err != nil {
			logger.Warnf("ListInterfaces: failed to parse %s: %v", name, err)
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

	logger.Infof("ListInterfaces: found %d interfaces", len(interfaces))
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
		logger.Warnf("%s: GetPublicIP returned empty string", logPrefix)
	}

	gatewayDNS, _ := getGatewayForInterfaceIPv4(egressNic)
	if gatewayDNS == "" {
		logger.Debugf("%s: no gateway DNS found for %s (optional - will use interface DNS if configured)", logPrefix, egressNic)
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
			logger.Warnf("AddInterface: peer %s has no AllowedIPs, skipping export", peer.PublicKey)
			continue
		}

		peerOffset := ipMgr.extractHostOffset(peer.AllowedIPs[0])
		if peerOffset < 0 {
			logger.Warnf("AddInterface: peer %s has invalid AllowedIP %q, skipping export", peer.PublicKey, peer.AllowedIPs[0])
			continue
		}

		if _, err := ExportPeerConfig(name, peer, cfg, publicIP, peerOffset, gatewayDNS); err != nil {
			logger.Warnf("AddInterface: failed to export config for peer %s: %v", peer.PublicKey, err)
		}
	}

	return nil
}

func bringUpInterfaceWithNAT(name, egressNic, subnet string) error {
	if _, err := UpInterface([]string{name}); err != nil {
		return err
	}

	if err := SetupNAT(name, egressNic, subnet); err != nil {
		if _, downErr := DownInterface([]string{name}); downErr != nil {
			logger.Warnf("AddInterface: failed to bring down %s after NAT failure: %v", name, downErr)
		}
		return fmt.Errorf("setup NAT: %w", err)
	}

	if err := SaveNATConfig(name, egressNic, subnet); err != nil {
		logger.Warnf("AddInterface: failed to save NAT config for %s: %v", name, err)
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
		logger.Warnf("AddPeer: GetPublicIP returned empty string")
	}

	gatewayDNS, _ := getDefaultGatewayIPv4()
	if gatewayDNS == "" {
		logger.Debugf("AddPeer: no default gateway DNS found (optional - will use interface DNS if configured)")
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
			logger.Warnf("AddPeer: rollback failed, could not remove peer config: %v", rmErr)
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
			logger.Warnf("AddPeer: parse allowed IP %s failed: %v", ipStr, parseErr)
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
		logger.Warnf("RemovePeerByName: parse public key failed: %v", err)
		return
	}

	client, err := wgctrl.New()
	if err != nil {
		logger.Warnf("RemovePeerByName: wgctrl.New failed: %v", err)
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
		logger.Warnf("RemovePeerByName: failed to remove peer from running interface: %v", err)
		return
	}

	logger.Infof("RemovePeerByName: removed peer from running interface %s", interfaceName)
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
			logger.Warnf("ListPeers: failed to parse %s: %v", file, err)
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
		logger.Warnf("ListPeers: could not parse main config for %s to map peer keys: %v", interfaceName, err)
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
		logger.Errorf("AddInterface: invalid arguments: %v", err)
		return nil, err
	}

	peers, err := resolveInterfacePeers(req)
	if err != nil {
		logger.Errorf("AddInterface: generate peers failed: %v", err)
		return nil, fmt.Errorf("generate peers: %w", err)
	}

	privKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		logger.Errorf("AddInterface: generate private key failed: %v", err)
		return nil, fmt.Errorf("generate private key: %w", err)
	}

	cfg := buildInterfaceConfig(req, privKey.String(), peers)
	publicIP, gatewayDNS := readInterfaceEndpointInfo("AddInterface", req.egressNic)
	if err := exportInterfacePeerConfigs(req.name, peers, cfg, req.addresses[0], publicIP, gatewayDNS); err != nil {
		logger.Errorf("AddInterface: init IP manager for peer export failed: %v", err)
		return nil, err
	}

	// Write main config (without PostUp/PostDown - we manage NAT programmatically)
	if err := WriteWireGuardConfig(configPath(req.name), cfg); err != nil {
		logger.Errorf("AddInterface: write config failed for %s: %v", req.name, err)
		return nil, fmt.Errorf("write config: %w", err)
	}

	subnet := req.addresses[0]
	if err := bringUpInterfaceWithNAT(req.name, req.egressNic, subnet); err != nil {
		logger.Errorf("AddInterface: failed to setup interface %s: %v", req.name, err)
		return nil, err
	}

	logger.Infof("AddInterface: interface %s created and brought up with NAT", req.name)

	return map[string]any{
		"status":      "created",
		"private_key": privKey.String(),
		"public_key":  privKey.PublicKey().String(),
		"peers":       peers,
	}, nil
}

func RemoveInterface(args []string) (any, error) {
	if len(args) < 1 {
		logger.Errorf("RemoveInterface: invalid arguments: %v", args)
		return nil, fmt.Errorf("usage: remove_interface <name>")
	}

	name := args[0]
	if err := validateInterfaceName(name); err != nil {
		logger.Errorf("RemoveInterface: invalid interface name %q: %v", name, err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	logger.Infof("RemoveInterface: removing interface %s", name)

	// Cleanup NAT rules before bringing down the interface
	if natCfg, err := LoadNATConfig(name); err != nil {
		logger.Warnf("RemoveInterface: failed to load NAT config for %s: %v", name, err)
	} else if natCfg != nil {
		if err := CleanupNAT(name, natCfg.EgressNic, natCfg.Subnet); err != nil {
			logger.Warnf("RemoveInterface: failed to cleanup NAT for %s: %v", name, err)
		}
		if err := RemoveNATConfig(name); err != nil {
			logger.Warnf("RemoveInterface: failed to remove NAT config for %s: %v", name, err)
		}
	}

	// Best-effort: bring it down, but don't abort on failure.
	if _, err := DownInterface([]string{name}); err != nil {
		logger.Warnf("RemoveInterface: failed to bring down %s; continuing with removal: %v", name, err)
	} else {
		logger.Infof("RemoveInterface: interface %s brought down before removal", name)
	}

	// Remove config file
	cfgPath := configPath(name)
	if err := os.Remove(cfgPath); err != nil {
		if os.IsNotExist(err) {
			logger.Warnf("RemoveInterface: config file already missing for %s (%s)", name, cfgPath)
		} else {
			logger.Errorf("RemoveInterface: failed to remove config for %s (%s): %v", name, cfgPath, err)
			return nil, fmt.Errorf("remove config: %w", err)
		}
	} else {
		logger.Infof("RemoveInterface: removed config file for %s (%s)", name, cfgPath)
	}

	// Remove peer directory (best-effort)
	peerDir := peerDirPath(name)
	if err := os.RemoveAll(peerDir); err != nil && !os.IsNotExist(err) {
		logger.Warnf("RemoveInterface: could not remove peer dir %s: %v", peerDir, err)
	} else {
		logger.Infof("RemoveInterface: removed peer directory for %s (%s)", name, peerDir)
	}

	logger.Infof("RemoveInterface: interface %s removed", name)
	return "removed", nil
}

func AddPeer(args []string) (any, error) {
	if len(args) < 1 {
		logger.Errorf("AddPeer: invalid arguments: %v", args)
		return nil, fmt.Errorf("usage: add_peer <interface>")
	}

	interfaceName := args[0]
	if err := validateInterfaceName(interfaceName); err != nil {
		logger.Errorf("AddPeer: invalid interface name %q: %v", interfaceName, err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	logger.Infof("AddPeer: adding peer to %s", interfaceName)

	// Read current config
	cfg, err := ParseWireGuardConfig(configPath(interfaceName))
	if err != nil {
		logger.Errorf("AddPeer: read config failed for %s: %v", interfaceName, err)
		return nil, fmt.Errorf("read config: %w", err)
	}

	peer, nextIP, err := createNextPeer(cfg)
	if err != nil {
		logger.Errorf("AddPeer: create peer failed: %v", err)
		return nil, err
	}

	if err := exportAddedPeer(interfaceName, peer, cfg); err != nil {
		logger.Errorf("AddPeer: export peer config failed: %v", err)
		return nil, err
	}

	if err := writePeerConfig(interfaceName, cfg, peer); err != nil {
		logger.Errorf("AddPeer: write config failed for %s: %v", interfaceName, err)
		return nil, err
	}

	if err := applyPeerToRunningInterface(interfaceName, peer); err != nil {
		logger.Errorf("AddPeer: failed to apply peer to running interface: %v", err)
		return nil, err
	}

	if isInterfaceUp(interfaceName) {
		logger.Infof("AddPeer: dynamically added %s with IP %s to %s", peer.Name, nextIP, interfaceName)
	} else {
		logger.Infof("AddPeer: added %s with IP %s to %s config (interface not running)", peer.Name, nextIP, interfaceName)
	}
	return map[string]any{
		"peer_name":  peer.Name,
		"public_key": peer.PublicKey,
		"allowed_ip": nextIP,
	}, nil
}

func RemovePeerByName(args []string) (any, error) {
	if len(args) < 2 {
		logger.Errorf("RemovePeerByName: invalid arguments: %v", args)
		return nil, fmt.Errorf("usage: remove_peer <interface> <peer>")
	}

	interfaceName := args[0]
	peerName := args[1]
	if err := validateInterfaceName(interfaceName); err != nil {
		logger.Errorf("RemovePeerByName: invalid interface name %q: %v", interfaceName, err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	if err := validateInterfaceName(peerName); err != nil {
		logger.Errorf("RemovePeerByName: invalid peer name %q: %v", peerName, err)
		return nil, fmt.Errorf("invalid peer name: %w", err)
	}
	logger.Infof("RemovePeerByName: removing peer %s from %s", peerName, interfaceName)

	peerPath, allowedIP, err := readPeerAllowedIP(interfaceName, peerName)
	if err != nil {
		logger.Errorf("RemovePeerByName: parse peer config %s failed: %v", peerPath, err)
		return nil, err
	}

	// Read main config
	cfg, err := ParseWireGuardConfig(configPath(interfaceName))
	if err != nil {
		logger.Errorf("RemovePeerByName: read main config for %s failed: %v", interfaceName, err)
		return nil, fmt.Errorf("read main config: %w", err)
	}

	cfg, removedPeerPubKey, found := removePeerFromConfig(cfg, allowedIP)
	if !found {
		logger.Errorf("RemovePeerByName: peer with IP %s not found in %s", allowedIP, interfaceName)
		return nil, fmt.Errorf("peer not found with IP %s", allowedIP)
	}

	// Write updated config
	if err := WriteWireGuardConfig(configPath(interfaceName), cfg); err != nil {
		logger.Errorf("RemovePeerByName: write updated config failed: %v", err)
		return nil, fmt.Errorf("write config: %w", err)
	}

	removePeerFromRunningInterface(interfaceName, removedPeerPubKey)

	// Remove peer config file
	if err := os.Remove(peerPath); err != nil && !os.IsNotExist(err) {
		logger.Warnf("RemovePeerByName: could not remove peer config %s: %v", peerPath, err)
	} else {
		logger.Infof("RemovePeerByName: removed peer config file %s", peerPath)
	}

	logger.Infof("RemovePeerByName: peer %s removed from %s", peerName, interfaceName)
	return "removed", nil
}

func ListPeers(args []string) (any, error) {
	if len(args) < 1 {
		logger.Errorf("ListPeers: invalid arguments: %v", args)
		return nil, fmt.Errorf("usage: list_exported_peers <interface>")
	}

	interfaceName := args[0]
	if err := validateInterfaceName(interfaceName); err != nil {
		logger.Errorf("ListPeers: invalid interface name %q: %v", interfaceName, err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	logger.Debugf("ListPeers: listing exported peers for %s", interfaceName)

	peers, err := loadExportedPeers(interfaceName)
	if err != nil {
		logger.Errorf("ListPeers: failed to read exported peers for %s: %v", interfaceName, err)
		return nil, err
	}

	mergeConfiguredPeerPublicKeys(interfaceName, peers)

	statsByPub, err := loadPeerRuntimeStats(interfaceName)
	if err != nil {
		logger.Warnf("ListPeers: wgctrl.New failed (device may be down). Returning peers without stats: %v", err)
		logger.Infof("ListPeers: found %d exported peers for %s", len(peers), interfaceName)
		return peers, nil
	}
	applyPeerRuntimeStats(peers, statsByPub)

	logger.Infof("ListPeers: found %d exported peers for %s (with runtime stats + bps)", len(peers), interfaceName)
	return peers, nil
}

func UpInterface(args []string) (any, error) {
	if len(args) < 1 {
		logger.Errorf("UpInterface: invalid arguments: %v", args)
		return nil, fmt.Errorf("usage: up_interface <name>")
	}
	name := args[0]
	if err := validateInterfaceName(name); err != nil {
		logger.Errorf("UpInterface: invalid interface name %q: %v", name, err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	cmd := exec.Command("/usr/bin/wg-quick", "up", name)

	// Ensure real/effective/saved IDs are 0 in the child
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: 0, Gid: 0},
	}

	// predictable env
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	cmd.Dir = "/"

	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf("UpInterface: failed to bring up %s: %v (%s)", name, err, string(out))
		return nil, fmt.Errorf("bring up interface: %w", err)
	}

	logger.Infof("UpInterface: interface %s brought up", name)
	return map[string]any{
		"status": "on",
		"output": string(out),
	}, nil
}

func DownInterface(args []string) (any, error) {
	if len(args) < 1 {
		logger.Errorf("DownInterface: invalid arguments: %v", args)
		return nil, fmt.Errorf("usage: down_interface <name>")
	}

	name := args[0]
	if err := validateInterfaceName(name); err != nil {
		logger.Errorf("DownInterface: invalid interface name %q: %v", name, err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	cmd := exec.Command("/usr/bin/wg-quick", "down", name)

	// Ensure real/effective/saved IDs are 0 in the child
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: 0, Gid: 0},
	}

	// predictable env
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	cmd.Dir = "/"

	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf("DownInterface: failed to bring down %s: %v (%s)", name, err, string(out))
		return nil, fmt.Errorf("bring down interface: %w", err)
	}

	logger.Infof("DownInterface: interface %s brought down", name)
	return map[string]any{
		"status": "off",
		"output": string(out),
	}, nil
}

func PeerQRCode(args []string) (any, error) {
	if len(args) < 2 {
		logger.Errorf("PeerQRCode: invalid arguments: %v", args)
		return nil, fmt.Errorf("usage: peer_qrcode <interface> <peername>")
	}

	if err := validateInterfaceName(args[0]); err != nil {
		logger.Errorf("PeerQRCode: invalid interface name %q: %v", args[0], err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	if err := validateInterfaceName(args[1]); err != nil {
		logger.Errorf("PeerQRCode: invalid peer name %q: %v", args[1], err)
		return nil, fmt.Errorf("invalid peer name: %w", err)
	}

	peerPath := peerConfigPath(args[0], args[1])
	rawConfig, err := os.ReadFile(peerPath)
	if err != nil {
		logger.Errorf("PeerQRCode: read peer config %s failed: %v", peerPath, err)
		return nil, fmt.Errorf("read peer config: %w", err)
	}

	// Generate QR code
	png, err := qrcode.Encode(string(rawConfig), qrcode.Medium, 256)
	if err != nil {
		logger.Errorf("PeerQRCode: QR encode failed for %s: %v", peerPath, err)
		return nil, fmt.Errorf("generate QR code: %w", err)
	}

	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	logger.Infof("PeerQRCode: generated QR for %s", peerPath)
	return map[string]string{"qrcode": dataURI}, nil
}

func PeerConfigDownload(args []string) (any, error) {
	if len(args) < 2 {
		logger.Errorf("PeerConfigDownload: invalid arguments: %v", args)
		return nil, fmt.Errorf("usage: peer_config_download <interface> <peername>")
	}

	if err := validateInterfaceName(args[0]); err != nil {
		logger.Errorf("PeerConfigDownload: invalid interface name %q: %v", args[0], err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	if err := validateInterfaceName(args[1]); err != nil {
		logger.Errorf("PeerConfigDownload: invalid peer name %q: %v", args[1], err)
		return nil, fmt.Errorf("invalid peer name: %w", err)
	}

	peerPath := peerConfigPath(args[0], args[1])
	data, err := os.ReadFile(peerPath)
	if err != nil {
		logger.Errorf("PeerConfigDownload: read peer config %s failed: %v", peerPath, err)
		return nil, fmt.Errorf("read peer config: %w", err)
	}

	logger.Infof("PeerConfigDownload: served %d bytes from %s", len(data), peerPath)
	return map[string]string{"config": string(data)}, nil
}

func GetKeys(args []string) (any, error) {
	if len(args) < 1 {
		logger.Errorf("GetKeys: invalid arguments: %v", args)
		return nil, fmt.Errorf("usage: get_keys <interface>")
	}

	if err := validateInterfaceName(args[0]); err != nil {
		logger.Errorf("GetKeys: invalid interface name %q: %v", args[0], err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}

	cfg, err := ParseWireGuardConfig(configPath(args[0]))
	if err != nil {
		logger.Errorf("GetKeys: read config failed for %s: %v", args[0], err)
		return nil, fmt.Errorf("read config: %w", err)
	}

	key, err := wgtypes.ParseKey(cfg.PrivateKey)
	if err != nil {
		logger.Errorf("GetKeys: parse private key failed: %v", err)
		return nil, fmt.Errorf("parse key: %w", err)
	}

	logger.Infof("GetKeys: provided keys for %s", args[0])
	return map[string]string{
		"private_key": cfg.PrivateKey,
		"public_key":  key.PublicKey().String(),
	}, nil
}

func EnableInterface(args []string) (any, error) {
	if len(args) < 1 {
		logger.Errorf("EnableInterface: invalid arguments: %v", args)
		return nil, fmt.Errorf("usage: enable_interface <name>")
	}

	name := args[0]
	if err := validateInterfaceName(name); err != nil {
		logger.Errorf("EnableInterface: invalid interface name %q: %v", name, err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}

	serviceName := fmt.Sprintf("wg-quick@%s.service", name)
	if err := systemd.EnableUnit(serviceName); err != nil {
		logger.Errorf("EnableInterface: failed to enable %s: %v", serviceName, err)
		return nil, fmt.Errorf("enable interface: %w", err)
	}

	logger.Infof("EnableInterface: %s enabled for boot persistence", name)
	return map[string]any{
		"status": "enabled",
		"output": "enabled via systemd D-Bus",
	}, nil
}

func DisableInterface(args []string) (any, error) {
	if len(args) < 1 {
		logger.Errorf("DisableInterface: invalid arguments: %v", args)
		return nil, fmt.Errorf("usage: disable_interface <name>")
	}

	name := args[0]
	if err := validateInterfaceName(name); err != nil {
		logger.Errorf("DisableInterface: invalid interface name %q: %v", name, err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}

	serviceName := fmt.Sprintf("wg-quick@%s.service", name)
	if err := systemd.DisableUnit(serviceName); err != nil {
		logger.Errorf("DisableInterface: failed to disable %s: %v", serviceName, err)
		return nil, fmt.Errorf("disable interface: %w", err)
	}

	logger.Infof("DisableInterface: %s disabled from boot persistence", name)
	return map[string]any{
		"status": "disabled",
		"output": "disabled via systemd D-Bus",
	}, nil
}
