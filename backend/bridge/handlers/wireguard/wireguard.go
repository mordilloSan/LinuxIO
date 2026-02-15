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

	"github.com/mordilloSan/LinuxIO/backend/common/utils"
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

func AddInterface(args []string) (any, error) {
	if len(args) < 4 {
		logger.Errorf("AddInterface: invalid arguments: %v", args)
		return nil, fmt.Errorf("usage: add_interface <name> <addresses> <listenPort> <egressNic> [dns] [mtu] [peers_json] [numPeers]")
	}

	name := args[0]
	if err := validateInterfaceName(name); err != nil {
		logger.Errorf("AddInterface: invalid interface name %q: %v", name, err)
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}

	addresses := normalizeAddresses(parseCSV(args[1]))
	if len(addresses) == 0 {
		logger.Errorf("AddInterface: no addresses provided")
		return nil, fmt.Errorf("at least one address is required")
	}

	listenPort, err := strconv.Atoi(args[2])
	if err != nil {
		logger.Errorf("AddInterface: invalid listen port %q: %v", args[2], err)
		return nil, fmt.Errorf("invalid listen port: %w", err)
	}
	egressNic := args[3]

	// Optional parameters
	dns := parseOptionalCSV(args, 4)
	mtu := parseOptionalInt(args, 5, 0)
	peers := parseOptionalPeers(args, 6)
	numPeers := parseOptionalInt(args, 7, 0)

	// Auto-generate peers if requested
	if len(peers) == 0 && numPeers > 0 {
		peers, err = generatePeers(addresses[0], numPeers)
		if err != nil {
			logger.Errorf("AddInterface: generate peers failed: %v", err)
			return nil, fmt.Errorf("generate peers: %w", err)
		}
	}

	// Generate server keys
	privKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		logger.Errorf("AddInterface: generate private key failed: %v", err)
		return nil, fmt.Errorf("generate private key: %w", err)
	}

	cfg := InterfaceConfig{
		PrivateKey: privKey.String(),
		Address:    addresses,
		ListenPort: listenPort,
		DNS:        dns,
		MTU:        mtu,
		Peers:      peers,
	}

	// Get public IP once
	publicIP, _ := utils.GetPublicIP()
	if publicIP == "" {
		logger.Warnf("AddInterface: GetPublicIP returned empty string")
	}

	// Prefer the gateway of the selected egress nic
	gatewayDNS, _ := getGatewayForInterfaceIPv4(egressNic)
	if gatewayDNS == "" {
		logger.Debugf("AddInterface: no gateway DNS found for %s (optional - will use interface DNS if configured)", egressNic)
	}

	// Export peer configs (need ipManager for consistent offset-based naming)
	if len(peers) > 0 {
		ipMgr, err := newIPManager(addresses[0])
		if err != nil {
			logger.Errorf("AddInterface: init IP manager for peer export failed: %v", err)
			return nil, fmt.Errorf("init IP manager: %w", err)
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
	}

	// Write main config (without PostUp/PostDown - we manage NAT programmatically)
	if err := WriteWireGuardConfig(configPath(name), cfg); err != nil {
		logger.Errorf("AddInterface: write config failed for %s: %v", name, err)
		return nil, fmt.Errorf("write config: %w", err)
	}

	// Bring up interface
	if _, err := UpInterface([]string{name}); err != nil {
		logger.Errorf("AddInterface: failed to bring up %s: %v", name, err)
		return nil, err
	}

	// Setup NAT rules using iptables library
	subnet := addresses[0]
	if err := SetupNAT(name, egressNic, subnet); err != nil {
		logger.Errorf("AddInterface: failed to setup NAT for %s: %v", name, err)
		// Bring down interface since NAT setup failed
		if _, downErr := DownInterface([]string{name}); downErr != nil {
			logger.Warnf("AddInterface: failed to bring down %s after NAT failure: %v", name, downErr)
		}
		return nil, fmt.Errorf("setup NAT: %w", err)
	}

	// Save NAT config for cleanup later
	if err := SaveNATConfig(name, egressNic, subnet); err != nil {
		logger.Warnf("AddInterface: failed to save NAT config for %s: %v", name, err)
	}

	logger.Infof("AddInterface: interface %s created and brought up with NAT", name)

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

	if len(cfg.Address) == 0 {
		logger.Errorf("AddPeer: interface %s has no address", interfaceName)
		return nil, fmt.Errorf("interface has no address")
	}

	// Initialize IP manager
	ipMgr, err := newIPManager(cfg.Address[0])
	if err != nil {
		logger.Errorf("AddPeer: init IP manager failed: %v", err)
		return nil, fmt.Errorf("init IP manager: %w", err)
	}

	// Find next available IP
	nextIP, peerNumber, err := ipMgr.findNextAvailable(cfg.Peers)
	if err != nil {
		logger.Errorf("AddPeer: find next available IP failed: %v", err)
		return nil, err
	}

	// Generate keys
	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		logger.Errorf("AddPeer: generate private key failed: %v", err)
		return nil, fmt.Errorf("generate private key: %w", err)
	}

	peer := PeerConfig{
		PublicKey:           priv.PublicKey().String(),
		PrivateKey:          priv.String(),
		AllowedIPs:          []string{nextIP},
		PersistentKeepalive: defaultKeepalive,
		Name:                fmt.Sprintf("Peer%d", peerNumber),
	}

	// Export peer config first (before modifying main config)
	// This ensures we don't end up with a peer in main config but no exported file
	publicIP, _ := utils.GetPublicIP()
	if publicIP == "" {
		logger.Warnf("AddPeer: GetPublicIP returned empty string")
	}
	gatewayDNS, _ := getDefaultGatewayIPv4()
	if gatewayDNS == "" {
		logger.Debugf("AddPeer: no default gateway DNS found (optional - will use interface DNS if configured)")
	}

	// Add peer to config temporarily for export (needed for server pubkey)
	cfgWithPeer := cfg
	cfgWithPeer.Peers = append(cfgWithPeer.Peers, peer)
	if _, err := ExportPeerConfig(interfaceName, peer, cfgWithPeer, publicIP, peerNumber, gatewayDNS); err != nil {
		logger.Errorf("AddPeer: export peer config failed: %v", err)
		return nil, fmt.Errorf("export peer config: %w", err)
	}

	// Now update main config (write to disk)
	cfg.Peers = append(cfg.Peers, peer)
	if err := WriteWireGuardConfig(configPath(interfaceName), cfg); err != nil {
		logger.Errorf("AddPeer: write config failed for %s: %v", interfaceName, err)
		// Rollback: remove the exported peer config
		if rmErr := os.Remove(peerConfigPath(interfaceName, peer.Name)); rmErr != nil {
			logger.Warnf("AddPeer: rollback failed, could not remove peer config: %v", rmErr)
		}
		return nil, fmt.Errorf("write config: %w", err)
	}

	// Apply peer to running interface using wgctrl (no restart needed)
	if isInterfaceUp(interfaceName) {
		pubKey, err := wgtypes.ParseKey(peer.PublicKey)
		if err != nil {
			logger.Errorf("AddPeer: parse public key failed: %v", err)
			return nil, fmt.Errorf("parse public key: %w", err)
		}

		var allowedIPs []net.IPNet
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
			logger.Errorf("AddPeer: wgctrl.New failed: %v", err)
			return nil, fmt.Errorf("create wgctrl client: %w", err)
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
			logger.Errorf("AddPeer: ConfigureDevice failed for %s: %v", interfaceName, err)
			return nil, fmt.Errorf("configure device: %w", err)
		}

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

	// Read peer config to get AllowedIP
	peerPath := peerConfigPath(interfaceName, peerName)

	// Extract Address from peer config
	iniFile, err := ini.Load(peerPath)
	if err != nil {
		logger.Errorf("RemovePeerByName: parse peer config %s failed: %v", peerPath, err)
		return nil, fmt.Errorf("parse peer config: %w", err)
	}

	allowedIP := iniFile.Section("Interface").Key("Address").String()
	if allowedIP == "" {
		logger.Errorf("RemovePeerByName: peer config %s missing Address", peerPath)
		return nil, fmt.Errorf("peer config missing Address")
	}

	// Read main config
	cfg, err := ParseWireGuardConfig(configPath(interfaceName))
	if err != nil {
		logger.Errorf("RemovePeerByName: read main config for %s failed: %v", interfaceName, err)
		return nil, fmt.Errorf("read main config: %w", err)
	}

	// Remove peer from config and capture its public key
	found := false
	var removedPeerPubKey string
	newPeers := make([]PeerConfig, 0, len(cfg.Peers))
	for _, p := range cfg.Peers {
		// Check if this peer matches the AllowedIP
		match := false
		if slices.Contains(p.AllowedIPs, allowedIP) {
			match = true
			found = true
			removedPeerPubKey = p.PublicKey
		}
		if !match {
			newPeers = append(newPeers, p)
		}
	}

	if !found {
		logger.Errorf("RemovePeerByName: peer with IP %s not found in %s", allowedIP, interfaceName)
		return nil, fmt.Errorf("peer not found with IP %s", allowedIP)
	}

	cfg.Peers = newPeers

	// Write updated config
	if err := WriteWireGuardConfig(configPath(interfaceName), cfg); err != nil {
		logger.Errorf("RemovePeerByName: write updated config failed: %v", err)
		return nil, fmt.Errorf("write config: %w", err)
	}

	// Remove peer from running interface if active
	if isInterfaceUp(interfaceName) && removedPeerPubKey != "" {
		pubKey, err := wgtypes.ParseKey(removedPeerPubKey)
		if err != nil {
			logger.Warnf("RemovePeerByName: parse public key failed: %v", err)
		} else {
			client, err := wgctrl.New()
			if err != nil {
				logger.Warnf("RemovePeerByName: wgctrl.New failed: %v", err)
			} else {
				defer client.Close()
				peerCfg := wgtypes.PeerConfig{
					PublicKey: pubKey,
					Remove:    true,
				}
				if err := client.ConfigureDevice(interfaceName, wgtypes.Config{
					Peers: []wgtypes.PeerConfig{peerCfg},
				}); err != nil {
					logger.Warnf("RemovePeerByName: failed to remove peer from running interface: %v", err)
				} else {
					logger.Infof("RemovePeerByName: removed peer from running interface %s", interfaceName)
				}
			}
		}
	}

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

	// 1) Read exported peer configs from disk (as before)
	pattern := filepath.Join(peerDirPath(interfaceName), "*"+configExt)
	files, err := filepath.Glob(pattern)
	if err != nil {
		logger.Errorf("ListPeers: glob failed for %s: %v", pattern, err)
		return nil, fmt.Errorf("list peer configs: %w", err)
	}

	peers := make([]PeerInfo, 0, len(files))

	for _, file := range files {
		iniFile, loadErr := ini.Load(file)
		if loadErr != nil {
			logger.Warnf("ListPeers: failed to parse %s: %v", file, loadErr)
			continue
		}

		ifSec := iniFile.Section("Interface")
		peerSec := iniFile.Section("Peer")

		pc := PeerConfig{
			PrivateKey: ifSec.Key("PrivateKey").String(),
			AllowedIPs: parseCSV(ifSec.Key("Address").String()),
			// NOTE: exported file's [Peer] PublicKey is the SERVER key
			PublicKey:           peerSec.Key("PublicKey").String(),
			PresharedKey:        peerSec.Key("PresharedKey").String(),
			Endpoint:            peerSec.Key("Endpoint").String(),
			Name:                ifSec.Key("Name").String(),
			PersistentKeepalive: peerSec.Key("PersistentKeepalive").MustInt(0),
		}

		if pc.Name == "" {
			pc.Name = strings.TrimSuffix(filepath.Base(file), configExt)
		}

		peers = append(peers, PeerInfo{
			PeerConfig:        pc,
			LastHandshake:     "never",
			LastHandshakeUnix: 0,
			RxBytes:           0,
			TxBytes:           0,
			RxBps:             0,
			TxBps:             0,
		})
	}

	// 2) Load main interface config to map AllowedIP (/32) -> client PublicKey
	cfg, err := ParseWireGuardConfig(configPath(interfaceName))
	if err != nil {
		logger.Warnf("ListPeers: could not parse main config for %s to map peer keys: %v", interfaceName, err)
	} else {
		ipToPub := make(map[string]string, len(cfg.Peers))
		for _, p := range cfg.Peers {
			for _, ip := range p.AllowedIPs {
				ipToPub[ip] = p.PublicKey
			}
		}
		for i := range peers {
			if len(peers[i].AllowedIPs) > 0 {
				if pub, ok := ipToPub[peers[i].AllowedIPs[0]]; ok && pub != "" {
					peers[i].PublicKey = pub
				}
			}
		}
	}

	// 3) Query live stats (wgctrl) and merge by PublicKey; compute bps using cache
	client, err := wgctrl.New()
	if err != nil {
		logger.Warnf("ListPeers: wgctrl.New failed (device may be down). Returning peers without stats: %v", err)
		logger.Infof("ListPeers: found %d exported peers for %s", len(peers), interfaceName)
		return peers, nil
	}
	defer client.Close()

	dev, err := client.Device(interfaceName)
	if err != nil {
		logger.Warnf("ListPeers: client.Device(%s) failed (interface inactive?): %v", interfaceName, err)
		logger.Infof("ListPeers: found %d exported peers for %s (no runtime stats)", len(peers), interfaceName)
		return peers, nil
	}

	now := time.Now()
	statsByPub := make(map[string]struct {
		hs time.Time
		rx int64
		tx int64
	}, len(dev.Peers))

	ratesByPub := make(map[string]struct {
		rxBps float64
		txBps float64
	}, len(dev.Peers))

	for _, pr := range dev.Peers {
		pub := pr.PublicKey.String()
		statsByPub[pub] = struct {
			hs time.Time
			rx int64
			tx int64
		}{
			hs: pr.LastHandshakeTime,
			rx: pr.ReceiveBytes,
			tx: pr.TransmitBytes,
		}

		rxBps, txBps := computeRates(interfaceName, pub, pr.ReceiveBytes, pr.TransmitBytes, now)
		ratesByPub[pub] = struct {
			rxBps float64
			txBps float64
		}{rxBps: rxBps, txBps: txBps}
	}

	for i := range peers {
		pub := peers[i].PublicKey
		if st, ok := statsByPub[pub]; ok {
			if st.hs.IsZero() {
				peers[i].LastHandshake = "never"
				peers[i].LastHandshakeUnix = 0
			} else {
				peers[i].LastHandshake = st.hs.UTC().Format(time.RFC3339)
				peers[i].LastHandshakeUnix = st.hs.Unix()
			}
			peers[i].RxBytes = st.rx
			peers[i].TxBytes = st.tx

			if r, ok := ratesByPub[pub]; ok {
				peers[i].RxBps = r.rxBps
				peers[i].TxBps = r.txBps
			}
		}
	}

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

	serviceName := fmt.Sprintf("wg-quick@%s", name)
	cmd := exec.Command("systemctl", "enable", serviceName)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: 0, Gid: 0},
	}
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	cmd.Dir = "/"

	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf("EnableInterface: failed to enable %s: %v (%s)", serviceName, err, string(out))
		return nil, fmt.Errorf("enable interface: %w", err)
	}

	logger.Infof("EnableInterface: %s enabled for boot persistence", name)
	return map[string]any{
		"status": "enabled",
		"output": string(out),
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

	serviceName := fmt.Sprintf("wg-quick@%s", name)
	cmd := exec.Command("systemctl", "disable", serviceName)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: 0, Gid: 0},
	}
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	cmd.Dir = "/"

	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf("DisableInterface: failed to disable %s: %v (%s)", serviceName, err, string(out))
		return nil, fmt.Errorf("disable interface: %w", err)
	}

	logger.Infof("DisableInterface: %s disabled from boot persistence", name)
	return map[string]any{
		"status": "disabled",
		"output": string(out),
	}, nil
}
