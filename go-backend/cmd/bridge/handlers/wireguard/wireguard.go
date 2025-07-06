package wireguard

import (
	"encoding/json"
	"fmt"
	"go-backend/internal/logger"
	"go-backend/internal/utils"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gopkg.in/ini.v1"
)

// --- Types ---

type InterfaceConfig struct {
	PrivateKey string             `json:"private_key"`
	Address    []string           `json:"address"`
	ListenPort int                `json:"listen_port"`
	DNS        []string           `json:"dns"`
	MTU        int                `json:"mtu"`
	Peers      []utils.PeerConfig `json:"peers"`
}

type WireGuardInterfaceUI struct {
	Name        string `json:"name"`
	IsConnected string `json:"isConnected"`
	Address     string `json:"address"`
	Port        int    `json:"port"`
	PeerCount   int    `json:"peerCount"`
}

// --- Helpers ---

func configPath(name string) string {
	return filepath.Join("/etc/wireguard", name+".conf")
}

func peerNameFromPath(path string) string {
	base := filepath.Base(path) // e.g. "Peer3.conf"
	name := strings.TrimSuffix(base, ".conf")
	return name
}

func ParseWireGuardConfig(path string) (InterfaceConfig, error) {
	var cfg InterfaceConfig
	iniFile, err := ini.Load(path)
	if err != nil {
		return cfg, err
	}
	ifSec := iniFile.Section("Interface")
	cfg.PrivateKey = ifSec.Key("PrivateKey").String()
	cfg.Address = filterEmpty(strings.Split(ifSec.Key("Address").String(), ","))
	cfg.ListenPort, _ = ifSec.Key("ListenPort").Int()
	cfg.DNS = filterEmpty(strings.Split(ifSec.Key("DNS").String(), ","))
	cfg.MTU, _ = ifSec.Key("MTU").Int()
	peerIdx := 1
	for _, sec := range iniFile.Sections() {
		if sec.Name() != "Peer" && !strings.HasPrefix(sec.Name(), "Peer ") {
			continue
		}
		pc := utils.PeerConfig{
			PublicKey:    sec.Key("PublicKey").String(),
			PresharedKey: sec.Key("PresharedKey").String(),
			Endpoint:     sec.Key("Endpoint").String(),
			Name:         sec.Key("Name").String(),
		}
		if pc.Name == "" {
			pc.Name = fmt.Sprintf("peer%d", peerIdx)
		}
		pc.AllowedIPs = filterEmpty(strings.Split(sec.Key("AllowedIPs").String(), ","))
		pc.PersistentKeepalive, _ = sec.Key("PersistentKeepalive").Int()
		cfg.Peers = append(cfg.Peers, pc)
		peerIdx++
	}
	return cfg, nil
}

func WriteWireGuardConfig(path string, cfg InterfaceConfig) error {
	iniFile := ini.Empty()
	ifSec, _ := iniFile.NewSection("Interface")
	if len(cfg.Address) > 0 {
		if _, err := ifSec.NewKey("Address", strings.Join(cfg.Address, ",")); err != nil {
			logger.Warnf(" failed to set Address: %v", err)
		}
	}
	if cfg.ListenPort > 0 {
		if _, err := ifSec.NewKey("ListenPort", fmt.Sprint(cfg.ListenPort)); err != nil {
			logger.Warnf(" failed to set ListenPort: %v", err)
		}
	}
	if _, err := ifSec.NewKey("PrivateKey", cfg.PrivateKey); err != nil {
		logger.Warnf(" failed to set PrivateKey: %v", err)
	}
	if len(cfg.DNS) > 0 {
		if _, err := ifSec.NewKey("DNS", strings.Join(cfg.DNS, ",")); err != nil {
			logger.Warnf(" failed to set DNS: %v", err)
		}
	}
	if cfg.MTU > 0 {
		if _, err := ifSec.NewKey("MTU", fmt.Sprint(cfg.MTU)); err != nil {
			logger.Warnf(" failed to set MTU: %v", err)
		}
	}

	for _, peer := range cfg.Peers {
		psec, err := iniFile.NewSection("Peer")
		if err != nil {
			logger.Warnf(" failed to create [Peer] section: %v", err)
			continue
		}
		if _, err := psec.NewKey("PublicKey", peer.PublicKey); err != nil {
			logger.Warnf(" failed to set PublicKey: %v", err)
		}
		if peer.PresharedKey != "" {
			if _, err := psec.NewKey("PresharedKey", peer.PresharedKey); err != nil {
				logger.Warnf(" failed to set PresharedKey: %v", err)
			}
		}
		if len(peer.AllowedIPs) > 0 {
			if _, err := psec.NewKey("AllowedIPs", strings.Join(peer.AllowedIPs, ",")); err != nil {
				logger.Warnf(" failed to set AllowedIPs: %v", err)
			}
		}
		if peer.Endpoint != "" {
			if _, err := psec.NewKey("Endpoint", peer.Endpoint); err != nil {
				logger.Warnf(" failed to set Endpoint: %v", err)
			}
		}
		if peer.PersistentKeepalive > 0 {
			if _, err := psec.NewKey("PersistentKeepalive", fmt.Sprint(peer.PersistentKeepalive)); err != nil {
				logger.Warnf(" failed to set PersistentKeepalive: %v", err)
			}
		}
	}

	return iniFile.SaveTo(path)
}

func filterEmpty(items []string) []string {
	var out []string
	for _, s := range items {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func RemovePeerFromConfig(cfg *InterfaceConfig, publicKey string) bool {
	var newPeers []utils.PeerConfig
	removed := false
	for _, p := range cfg.Peers {
		if p.PublicKey == publicKey {
			removed = true
			continue
		}
		newPeers = append(newPeers, p)
	}
	cfg.Peers = newPeers
	return removed
}

func AddOrUpdatePeerInConfig(cfg *InterfaceConfig, newPeer utils.PeerConfig) {
	for i, p := range cfg.Peers {
		if p.PublicKey == newPeer.PublicKey {
			cfg.Peers[i] = newPeer
			return
		}
	}
	cfg.Peers = append(cfg.Peers, newPeer)
}

func ExportPeerConfigToDisk(interfaceName string, peer utils.PeerConfig, ifaceCfg InterfaceConfig, publicIP string) (string, error) {
	peerDir := filepath.Join("/etc/wireguard", interfaceName)
	if err := os.MkdirAll(peerDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create peer dir: %w", err)
	}
	// Find the next PeerN name
	files, err := filepath.Glob(filepath.Join(peerDir, "Peer*.conf"))
	if err != nil {
		return "", fmt.Errorf("failed to list existing peer configs: %w", err)
	}

	peerNum := len(files) + 1
	var peerPath string
	for {
		candidate := filepath.Join(peerDir, fmt.Sprintf("Peer%d.conf", peerNum))
		_, statErr := os.Stat(candidate)
		if os.IsNotExist(statErr) {
			peerPath = candidate
			break
		} else if statErr != nil {
			return "", fmt.Errorf("failed to stat peer config candidate: %w", statErr)
		}
		peerNum++
	}

	iniFile := ini.Empty()

	// [Interface]
	ifSec, err := iniFile.NewSection("Interface")
	if err != nil {
		return "", fmt.Errorf("failed to create [Interface] section: %w", err)
	}

	if len(peer.AllowedIPs) > 0 {
		if _, err := ifSec.NewKey("Address", peer.AllowedIPs[0]); err != nil {
			logger.Warnf(" failed to set Address: %v", err)
		}
	}
	if ifaceCfg.ListenPort > 0 {
		if _, err := ifSec.NewKey("ListenPort", fmt.Sprintf("%d", ifaceCfg.ListenPort)); err != nil {
			logger.Warnf(" failed to set ListenPort: %v", err)
		}
	}
	if peer.PrivateKey == "" {
		return "", fmt.Errorf("peer private key is empty")
	}
	if _, err := ifSec.NewKey("PrivateKey", peer.PrivateKey); err != nil {
		logger.Warnf(" failed to set PrivateKey: %v", err)
	}
	if len(ifaceCfg.DNS) > 0 {
		if _, err := ifSec.NewKey("DNS", strings.Join(ifaceCfg.DNS, ",")); err != nil {
			logger.Warnf(" failed to set DNS: %v", err)
		}
	}

	// [Peer]
	peerSec, err := iniFile.NewSection("Peer")
	if err != nil {
		return "", fmt.Errorf("failed to create [Peer] section: %w", err)
	}
	serverKey, err := wgtypes.ParseKey(ifaceCfg.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("failed to parse server private key: %w", err)
	}
	if _, err := peerSec.NewKey("PublicKey", serverKey.PublicKey().String()); err != nil {
		logger.Warnf(" failed to set PublicKey in peer section: %v", err)
	}
	if _, err := peerSec.NewKey("AllowedIPs", "0.0.0.0/0, ::/0"); err != nil {
		logger.Warnf(" failed to set AllowedIPs in peer section: %v", err)
	}

	// Use publicIP passed in (do NOT call getPublicIP)
	if publicIP != "" {
		if _, err := peerSec.NewKey("Endpoint", publicIP); err != nil {
			logger.Warnf(" failed to set Endpoint in peer section: %v", err)
		}
	}

	if peer.PresharedKey != "" {
		if _, err := peerSec.NewKey("PresharedKey", peer.PresharedKey); err != nil {
			logger.Warnf(" failed to set PresharedKey: %v", err)
		}
	}
	if peer.PersistentKeepalive > 0 {
		if _, err := peerSec.NewKey("PersistentKeepalive", fmt.Sprintf("%d", peer.PersistentKeepalive)); err != nil {
			logger.Warnf(" failed to set PersistentKeepalive: %v", err)
		}
	}

	if err := iniFile.SaveTo(peerPath); err != nil {
		return "", fmt.Errorf("failed to save peer config: %w", err)
	}
	return peerPath, nil
}

func isInterfaceUp(name string) bool {
	cmd := exec.Command("wg", "show", name)
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

func listExportedPeersByFilename(interfaceName string) ([]utils.PeerConfig, error) {
	peerDir := filepath.Join("/etc/wireguard", interfaceName)
	files, err := filepath.Glob(filepath.Join(peerDir, "*.conf"))
	if err != nil {
		return nil, fmt.Errorf("failed to list peer configs: %w", err)
	}

	var peers []utils.PeerConfig

	for _, file := range files {
		iniFile, err := ini.Load(file)
		if err != nil {
			logger.Warnf(" Failed to parse peer config %s: %v", file, err)
			continue
		}

		ifaceSec := iniFile.Section("Interface")
		peerSec := iniFile.Section("Peer")

		pc := utils.PeerConfig{
			PrivateKey:   ifaceSec.Key("PrivateKey").String(),
			AllowedIPs:   filterEmpty(strings.Split(ifaceSec.Key("Address").String(), ",")),
			PublicKey:    peerSec.Key("PublicKey").String(),
			PresharedKey: peerSec.Key("PresharedKey").String(),
			Endpoint:     peerSec.Key("Endpoint").String(),
			Name:         ifaceSec.Key("Name").String(),
		}
		if pc.Name == "" {
			pc.Name = peerNameFromPath(file)
		}
		pc.PersistentKeepalive, _ = peerSec.Key("PersistentKeepalive").Int()
		// AllowedIPs from Peer section (for client configs it's usually 0.0.0.0/0, ::/0)
		if allowed := peerSec.Key("AllowedIPs").String(); allowed != "" {
			pc.AllowedIPs = filterEmpty(strings.Split(allowed, ","))
		}

		peers = append(peers, pc)
	}

	return peers, nil
}

// --- Handler Implementations ---

func ListInterfaces(args []string) (any, error) {
	logger.Debugf(" Listing interfaces")
	files, err := filepath.Glob("/etc/wireguard/*.conf")
	if err != nil {
		logger.Errorf(" Failed to list interfaces: %v", err)
		return nil, err
	}

	var uiIfaces []WireGuardInterfaceUI

	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".conf")
		cfg, err := ParseWireGuardConfig(configPath(name))
		if err != nil {
			logger.Warnf(" Failed to parse config for %s: %v", name, err)
			continue // skip broken configs
		}
		status := isInterfaceUp(name)
		uiIfaces = append(uiIfaces, WireGuardInterfaceUI{
			Name: name,
			IsConnected: func() string {
				if status {
					return "Active"
				} else {
					return "Inactive"
				}
			}(),
			Address:   strings.Join(cfg.Address, ", "),
			Port:      cfg.ListenPort,
			PeerCount: len(cfg.Peers),
		})
	}
	logger.Infof(" Found interfaces: %d", len(uiIfaces))
	return uiIfaces, nil
}

func AddInterface(args []string) (any, error) {
	if len(args) < 4 {
		logger.Warnf(" add_interface: missing args")
		return nil, fmt.Errorf("usage: add_interface <name> <addresses> <listenPort> <egressNic> [dns] [mtu] [peers_json]")
	}

	name := args[0]
	address := filterEmpty(strings.Split(args[1], ","))
	listenPort, _ := strconv.Atoi(args[2])
	egressNic := args[3]

	dns := []string{}
	if len(args) > 4 && args[4] != "" {
		dns = filterEmpty(strings.Split(args[4], ","))
	}

	mtu := 0
	if len(args) > 5 && args[5] != "" {
		mtu, _ = strconv.Atoi(args[5])
	}

	var peers []utils.PeerConfig

	if len(args) > 6 && args[6] != "" && args[6] != "null" && args[6] != "[]" {
		if err := json.Unmarshal([]byte(args[6]), &peers); err != nil {
			return nil, fmt.Errorf("failed to parse peers JSON: %w", err)
		}
	}

	// Auto-generate peers if NumPeers > 0 and no peers were passed
	if len(peers) == 0 && len(args) > 7 {
		numPeers, _ := strconv.Atoi(args[7])
		if numPeers > 0 {
			for i := range numPeers {
				peerPriv, err := wgtypes.GeneratePrivateKey()
				if err != nil {
					return nil, fmt.Errorf("failed to generate peer key: %w", err)
				}
				peer := utils.PeerConfig{
					PublicKey:  peerPriv.PublicKey().String(),
					PrivateKey: peerPriv.String(),
					AllowedIPs: []string{
						fmt.Sprintf("%s%d/32", strings.TrimSuffix(address[0][:strings.LastIndex(address[0], ".")+1], "/24"), i+2),
					},
					PersistentKeepalive: 25,
				}
				peers = append(peers, peer)
			}
		}
	}

	privKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}
	privateKey := privKey.String()
	publicKey := privKey.PublicKey().String()

	cfg := InterfaceConfig{
		PrivateKey: privateKey,
		Address:    address,
		ListenPort: listenPort,
		DNS:        dns,
		MTU:        mtu,
		Peers:      peers,
	}

	// Fetch public IP ONCE
	publicIP, err := utils.GetPublicIP()
	if err != nil {
		logger.Warnf(" Failed to fetch public IP: %v", err)
		publicIP = ""
	}

	for _, peer := range peers {
		if _, err := ExportPeerConfigToDisk(name, peer, cfg, publicIP); err != nil {
			logger.Warnf(" Failed to export client config for peer %s: %v", peer.PublicKey, err)
		} else {
			logger.Infof(" Exported client config for peer %s", peer.PublicKey)
		}
	}

	if err := WriteWireGuardConfigWithPostUpDown(configPath(name), cfg, egressNic); err != nil {
		logger.Errorf(" Failed to write config for %s: %v", name, err)
		return nil, err
	}

	if _, err := UpInterface([]string{name}); err != nil {
		return nil, err // logging already done in UpInterface
	}

	logger.Infof(" Interface %s created and brought up", name)

	// ---- UPnP Port Mapping (Optional) ----
	//upnp.OpenRouterPort(egressNic, listenPort, name)

	return map[string]any{
		"status":      "created",
		"private_key": privateKey,
		"public_key":  publicKey,
		"peers":       peers,
	}, nil

}

func RemoveInterface(args []string) (any, error) {
	if len(args) < 1 {
		logger.Warnf(" remove_interface: missing name argument")
		return nil, fmt.Errorf("usage: remove_interface <name>")
	}
	name := args[0]
	logger.Infof(" Removing interface: %s", name)
	if _, err := DownInterface([]string{name}); err != nil {
		logger.Warnf(" Failed to bring down %s: %v", name, err)
		// Not returning, since removal should proceed regardless
	}

	if err := os.Remove(configPath(name)); err != nil {
		logger.Errorf(" Failed to remove config for %s: %v", name, err)
		return nil, err
	}
	// Remove all peer configs for this interface
	peerDir := filepath.Join("/etc/wireguard", name)
	if err := os.RemoveAll(peerDir); err == nil {
		logger.Infof(" Removed all peer configs in %s", peerDir)
	} else if !os.IsNotExist(err) {
		logger.Warnf(" Could not remove peer config dir %s: %v", peerDir, err)
	}
	logger.Infof(" Interface %s removed", name)
	return "removed", nil
}

func ListPeers(args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("usage: list_exported_peers <interface>")
	}
	peers, err := listExportedPeersByFilename(args[0])
	if err != nil {
		return nil, err
	}
	return peers, nil
}

func AddPeer(args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("usage: add_peer <interface>")
	}
	interfaceName := args[0]

	// 1. Parse config, get all existing peers and their AllowedIPs
	cfg, err := ParseWireGuardConfig(configPath(interfaceName))
	if err != nil {
		return nil, fmt.Errorf("failed to read config for %s: %v", interfaceName, err)
	}

	// 2. Get list of existing Peer*.conf files to determine next peer number
	peerDir := filepath.Join("/etc/wireguard", interfaceName)
	files, _ := os.ReadDir(peerDir)
	maxN := 0
	re := regexp.MustCompile(`^Peer(\d+)\.conf$`)
	for _, f := range files {
		m := re.FindStringSubmatch(f.Name())
		if len(m) == 2 {
			if n, err := strconv.Atoi(m[1]); err == nil && n > maxN {
				maxN = n
			}
		}
	}
	peerName := fmt.Sprintf("Peer%d", maxN+1)

	// 3. Auto-generate next available AllowedIP (e.g., 10.10.20.2/32, 10.10.20.3/32, ...)
	baseCIDR := cfg.Address[0] // e.g. "10.10.20.0/24"
	ipPrefix := strings.Join(strings.Split(baseCIDR, ".")[:3], ".") + "."
	usedIPs := map[string]bool{"0": true}
	for _, p := range cfg.Peers {
		for _, ip := range p.AllowedIPs {
			parts := strings.Split(ip, ".")
			if len(parts) == 4 {
				usedIPs[parts[3][:strings.Index(parts[3], "/")]] = true
			}
		}
	}
	nextIP := ""
	for i := 2; i < 255; i++ { // 10.10.20.2/32 .. 254
		key := strconv.Itoa(i)
		if !usedIPs[key] {
			nextIP = fmt.Sprintf("%s%d/32", ipPrefix, i)
			break
		}
	}
	if nextIP == "" {
		return nil, fmt.Errorf("no available IPs left")
	}

	// 4. Generate keypair
	priv, _ := wgtypes.GeneratePrivateKey()
	pub := priv.PublicKey().String()

	peer := utils.PeerConfig{
		PublicKey:           pub,
		AllowedIPs:          []string{nextIP},
		PersistentKeepalive: 25,
		Name:                peerName,
	}

	// 5. Append to config and write
	AddOrUpdatePeerInConfig(&cfg, peer)
	if err := WriteWireGuardConfig(configPath(interfaceName), cfg); err != nil {
		return nil, fmt.Errorf("failed to write config: %v", err)
	}

	// 6. Export client config file
	publicIP, _ := utils.GetPublicIP()
	_, err = ExportPeerConfigToDisk(interfaceName, peer, cfg, publicIP)
	if err != nil {
		return nil, fmt.Errorf("failed to write peer config: %v", err)
	}

	// 7. Optionally add peer live (wgctrl...)

	return map[string]any{
		"peer_name":  peerName,
		"public_key": pub,
		"allowed_ip": nextIP,
		// more fields as you wish
	}, nil
}

func RemovePeerByName(args []string) (any, error) {
	if len(args) < 2 {
		logger.Warnf("remove_peer: missing args")
		return nil, fmt.Errorf("usage: remove_peer <interface> <peer>")
	}
	interfaceName := args[0]
	peerName := args[1]
	peerDir := filepath.Join("/etc/wireguard", interfaceName)
	peerConfPath := filepath.Join(peerDir, peerName+".conf")
	mainCfgPath := filepath.Join("/etc/wireguard", interfaceName+".conf")

	// 1. Parse AllowedIPs from the peer conf
	rawPeer, err := os.ReadFile(peerConfPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read peer conf: %v", err)
	}
	allowedIP := extractKeyValue(string(rawPeer), "Address")
	if allowedIP == "" {
		return nil, fmt.Errorf("could not find Address (AllowedIP) in peer config")
	}

	// 2. Read main config
	rawMain, err := os.ReadFile(mainCfgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read main config: %v", err)
	}
	sections := strings.Split(string(rawMain), "[Peer]")

	// 3. Find and remove the matching [Peer] section
	var builder strings.Builder
	builder.WriteString(sections[0]) // Always include the [Interface] section
	removed := false
	pattern := regexp.MustCompile(fmt.Sprintf(`(?m)^\s*AllowedIPs\s*=\s*%s\b`, regexp.QuoteMeta(allowedIP)))

	for i := 1; i < len(sections); i++ {
		section := "[Peer]" + sections[i]
		if pattern.FindStringIndex(section) != nil {
			removed = true
			continue
		}
		builder.WriteString("[Peer]")
		builder.WriteString(sections[i])

	}

	if !removed {
		return nil, fmt.Errorf("no matching peer found for AllowedIPs=%s", allowedIP)
	}

	// 4. Write back the config
	if err := os.WriteFile(mainCfgPath, []byte(builder.String()), 0600); err != nil {
		return nil, fmt.Errorf("failed to write main config: %v", err)
	}

	// 5. Remove the exported peer conf file
	if err := os.Remove(peerConfPath); err != nil && !os.IsNotExist(err) {
		logger.Warnf("could not remove exported config %s: %v", peerConfPath, err)
	}

	return "removed", nil
}

// Improved, robust key extractor with regex
func extractKeyValue(s, key string) string {
	re := regexp.MustCompile(fmt.Sprintf(`(?m)^\s*%s\s*=\s*(.+)$`, regexp.QuoteMeta(key)))
	matches := re.FindStringSubmatch(s)
	if len(matches) == 2 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

func GetKeys(args []string) (any, error) {
	if len(args) < 1 {
		logger.Warnf(" get_keys: missing interface argument")
		return nil, fmt.Errorf("usage: get_keys <interface>")
	}
	name := args[0]
	logger.Infof(" Getting keys for interface: %s", name)
	cfg, err := ParseWireGuardConfig(configPath(name))
	if err != nil {
		logger.Errorf(" Failed to read config for %s: %v", name, err)
		return nil, err
	}
	priv := cfg.PrivateKey
	key, _ := wgtypes.ParseKey(priv)
	logger.Infof(" Keys for %s retrieved", name)
	return map[string]string{
		"private_key": priv,
		"public_key":  key.PublicKey().String(),
	}, nil
}

func WriteWireGuardConfigWithPostUpDown(path string, cfg InterfaceConfig, egressNic string) error {
	iniFile := ini.Empty(ini.LoadOptions{AllowNonUniqueSections: true})
	ifSec, err := iniFile.NewSection("Interface")
	if err != nil {
		logger.Warnf(" failed to create [Interface] section: %v", err)
	}
	if len(cfg.Address) > 0 {
		if _, err := ifSec.NewKey("Address", strings.Join(cfg.Address, ",")); err != nil {
			logger.Warnf(" failed to set Address: %v", err)
		}
	}
	if cfg.ListenPort > 0 {
		if _, err := ifSec.NewKey("ListenPort", fmt.Sprint(cfg.ListenPort)); err != nil {
			logger.Warnf(" failed to set ListenPort: %v", err)
		}
	}
	if _, err := ifSec.NewKey("PrivateKey", cfg.PrivateKey); err != nil {
		logger.Warnf(" failed to set PrivateKey: %v", err)
	}
	postUp := fmt.Sprintf(
		"iptables -A FORWARD -i %%i -j ACCEPT; iptables -t nat -A POSTROUTING -o %s -j MASQUERADE",
		egressNic,
	)
	postDown := fmt.Sprintf(
		"iptables -D FORWARD -i %%i -j ACCEPT; iptables -t nat -D POSTROUTING -o %s -j MASQUERADE",
		egressNic,
	)
	if _, err := ifSec.NewKey("PostUp", postUp); err != nil {
		logger.Warnf(" failed to set PostUp: %v", err)
	}
	if _, err := ifSec.NewKey("PostDown", postDown); err != nil {
		logger.Warnf(" failed to set PostDown: %v", err)
	}

	for _, peer := range cfg.Peers {
		psec, err := iniFile.NewSection("Peer")
		if err != nil {
			logger.Warnf(" failed to create [Peer] section: %v", err)
			continue
		}
		if _, err := psec.NewKey("PublicKey", peer.PublicKey); err != nil {
			logger.Warnf(" failed to set PublicKey: %v", err)
		}
		if peer.PresharedKey != "" {
			if _, err := psec.NewKey("PresharedKey", peer.PresharedKey); err != nil {
				logger.Warnf(" failed to set PresharedKey: %v", err)
			}
		}
		if len(peer.AllowedIPs) > 0 {
			if _, err := psec.NewKey("AllowedIPs", strings.Join(peer.AllowedIPs, ",")); err != nil {
				logger.Warnf(" failed to set AllowedIPs: %v", err)
			}
		}
		if peer.Endpoint != "" {
			if _, err := psec.NewKey("Endpoint", peer.Endpoint); err != nil {
				logger.Warnf(" failed to set Endpoint: %v", err)
			}
		}
		if peer.PersistentKeepalive > 0 {
			if _, err := psec.NewKey("PersistentKeepalive", fmt.Sprint(peer.PersistentKeepalive)); err != nil {
				logger.Warnf(" failed to set PersistentKeepalive: %v", err)
			}
		}
	}

	// Save the ini file first
	if err := iniFile.SaveTo(path); err != nil {
		return err
	}

	// --- Post-process to remove all backticks from the file ---
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := strings.ReplaceAll(string(raw), "`", "")
	err = os.WriteFile(path, []byte(content), 0600)
	if err != nil {
		return err
	}

	return nil
}

func UpInterface(args []string) (any, error) {
	if len(args) < 1 {
		logger.Warnf(" up_interface: missing name argument")
		return nil, fmt.Errorf("usage: up_interface <name>")
	}
	name := args[0]

	cmd := exec.Command("wg-quick", "up", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf(" Failed to bring up %s: %v (%s)", name, err, string(out))
		return nil, fmt.Errorf("failed to bring up interface: %v (%s)", err, string(out))
	}
	if len(out) > 0 {
		logger.Debugf(" wg-quick up output for %s: %s", name, string(out))
	}

	logger.Infof(" Interface %s brought up", name)

	return map[string]any{
		"status": "on",
		"output": string(out),
	}, nil
}

func DownInterface(args []string) (any, error) {
	if len(args) < 1 {
		logger.Warnf(" down_interface: missing name argument")
		return nil, fmt.Errorf("usage: down_interface <name>")
	}
	name := args[0]

	cmd := exec.Command("wg-quick", "down", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf(" Failed to bring down %s: %v (%s)", name, err, string(out))
		return nil, fmt.Errorf("failed to bring down interface: %v (%s)", err, string(out))
	}
	if len(out) > 0 {
		logger.Debugf(" wg-quick down output for %s: %s", name, string(out))
	}

	logger.Infof(" Interface %s brought down", name)

	return map[string]any{
		"status": "off",
		"output": string(out),
	}, nil
}
