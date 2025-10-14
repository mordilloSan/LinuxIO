package wireguard

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/skip2/go-qrcode"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gopkg.in/ini.v1"

	"github.com/mordilloSan/LinuxIO/backend/common/logger"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
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
	addresses := normalizeAddresses(parseCSV(args[1]))
	listenPort, _ := strconv.Atoi(args[2])
	egressNic := args[3]

	// Optional parameters
	dns := parseOptionalCSV(args, 4)
	mtu := parseOptionalInt(args, 5, 0)
	peers := parseOptionalPeers(args, 6)
	numPeers := parseOptionalInt(args, 7, 0)

	// Auto-generate peers if requested
	if len(peers) == 0 && numPeers > 0 {
		var err error
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
		logger.Warnf("AddInterface: could not determine gateway DNS for %s", egressNic)
	}

	// Export peer configs
	for _, peer := range peers {
		peerNumber := extractHostOctet(peer.AllowedIPs[0])
		if _, err := ExportPeerConfig(name, peer, cfg, publicIP, peerNumber, gatewayDNS); err != nil {
			logger.Warnf("AddInterface: failed to export config for peer %s: %v", peer.PublicKey, err)
		}
	}

	// Write main config
	if err := WriteWireGuardConfigWithPostUpDown(configPath(name), cfg, egressNic); err != nil {
		logger.Errorf("AddInterface: write config failed for %s: %v", name, err)
		return nil, fmt.Errorf("write config: %w", err)
	}

	// Bring up interface
	if _, err := UpInterface([]string{name}); err != nil {
		logger.Errorf("AddInterface: failed to bring up %s: %v", name, err)
		return nil, err
	}

	logger.Infof("AddInterface: interface %s created and brought up", name)

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
	logger.Infof("RemoveInterface: removing interface %s", name)

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
	priv, _ := wgtypes.GeneratePrivateKey()

	peer := PeerConfig{
		PublicKey:           priv.PublicKey().String(),
		PrivateKey:          priv.String(),
		AllowedIPs:          []string{nextIP},
		PersistentKeepalive: defaultKeepalive,
		Name:                fmt.Sprintf("Peer%d", peerNumber),
	}

	// Update config (write to disk)
	cfg.Peers = append(cfg.Peers, peer)
	if err := WriteWireGuardConfig(configPath(interfaceName), cfg); err != nil {
		logger.Errorf("AddPeer: write config failed for %s: %v", interfaceName, err)
		return nil, fmt.Errorf("write config: %w", err)
	}

	// Export peer config
	publicIP, _ := utils.GetPublicIP()
	if publicIP == "" {
		logger.Warnf("AddPeer: GetPublicIP returned empty string")
	}
	gatewayDNS, _ := getDefaultGatewayIPv4()
	if gatewayDNS == "" {
		logger.Warnf("AddPeer: could not determine default gateway DNS")
	}
	if _, err := ExportPeerConfig(interfaceName, peer, cfg, publicIP, peerNumber, gatewayDNS); err != nil {
		logger.Errorf("AddPeer: export peer config failed: %v", err)
		return nil, fmt.Errorf("export peer config: %w", err)
	}

	// Bounce interface to apply changes
	if _, err := DownInterface([]string{interfaceName}); err != nil {
		logger.Warnf("AddPeer: wg-quick down %s failed (continuing): %v", interfaceName, err)
	}
	if _, err := UpInterface([]string{interfaceName}); err != nil {
		logger.Errorf("AddPeer: wg-quick up %s failed after adding peer: %v", interfaceName, err)
		return nil, fmt.Errorf("restart interface: %w", err)
	}

	logger.Infof("AddPeer: added %s with IP %s to %s and restarted interface", peer.Name, nextIP, interfaceName)
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

	// Remove peer from config
	found := false
	newPeers := make([]PeerConfig, 0, len(cfg.Peers))
	for _, p := range cfg.Peers {
		// Check if this peer matches the AllowedIP
		match := false
		for _, ip := range p.AllowedIPs {
			if ip == allowedIP {
				match = true
				found = true
				break
			}
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
			logger.Warnf("ListPeers: failed to parse %s: %v", file, err)
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
