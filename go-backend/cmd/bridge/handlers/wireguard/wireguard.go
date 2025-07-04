package wireguard

import (
	"encoding/json"
	"fmt"
	"go-backend/internal/logger"
	"go-backend/internal/utils"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl"
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

func getPublicIP() (string, error) {
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			logger.Warnf("failed to close response body: %v", cerr)
		}
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(body))
	return ip, nil
}

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
			logger.Warnf("[wireguard] failed to set Address: %v", err)
		}
	}
	if cfg.ListenPort > 0 {
		if _, err := ifSec.NewKey("ListenPort", fmt.Sprint(cfg.ListenPort)); err != nil {
			logger.Warnf("[wireguard] failed to set ListenPort: %v", err)
		}
	}
	if _, err := ifSec.NewKey("PrivateKey", cfg.PrivateKey); err != nil {
		logger.Warnf("[wireguard] failed to set PrivateKey: %v", err)
	}
	if len(cfg.DNS) > 0 {
		if _, err := ifSec.NewKey("DNS", strings.Join(cfg.DNS, ",")); err != nil {
			logger.Warnf("[wireguard] failed to set DNS: %v", err)
		}
	}
	if cfg.MTU > 0 {
		if _, err := ifSec.NewKey("MTU", fmt.Sprint(cfg.MTU)); err != nil {
			logger.Warnf("[wireguard] failed to set MTU: %v", err)
		}
	}

	for _, peer := range cfg.Peers {
		psec, err := iniFile.NewSection("Peer")
		if err != nil {
			logger.Warnf("[wireguard] failed to create [Peer] section: %v", err)
			continue
		}
		if _, err := psec.NewKey("PublicKey", peer.PublicKey); err != nil {
			logger.Warnf("[wireguard] failed to set PublicKey: %v", err)
		}
		if peer.PresharedKey != "" {
			if _, err := psec.NewKey("PresharedKey", peer.PresharedKey); err != nil {
				logger.Warnf("[wireguard] failed to set PresharedKey: %v", err)
			}
		}
		if len(peer.AllowedIPs) > 0 {
			if _, err := psec.NewKey("AllowedIPs", strings.Join(peer.AllowedIPs, ",")); err != nil {
				logger.Warnf("[wireguard] failed to set AllowedIPs: %v", err)
			}
		}
		if peer.Endpoint != "" {
			if _, err := psec.NewKey("Endpoint", peer.Endpoint); err != nil {
				logger.Warnf("[wireguard] failed to set Endpoint: %v", err)
			}
		}
		if peer.PersistentKeepalive > 0 {
			if _, err := psec.NewKey("PersistentKeepalive", fmt.Sprint(peer.PersistentKeepalive)); err != nil {
				logger.Warnf("[wireguard] failed to set PersistentKeepalive: %v", err)
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

func removeExportedPeerConfig(peerDir, publicKey string) {
	files, _ := filepath.Glob(filepath.Join(peerDir, "Peer*.conf"))
	for _, file := range files {
		iniFile, err := ini.Load(file)
		if err != nil {
			continue
		}
		ifSec := iniFile.Section("Interface")
		if ifSec.Key("PublicKey").String() == publicKey {
			if err := os.Remove(file); err == nil {
				logger.Infof("[wireguard] Removed exported config %s", file)
			} else if !os.IsNotExist(err) {
				logger.Warnf("[wireguard] Could not remove exported config %s: %v", file, err)
			}
			return
		}
	}
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

func ExportPeerConfigToDisk(interfaceName string, peer utils.PeerConfig, ifaceCfg InterfaceConfig) (string, error) {
	peerDir := filepath.Join("/etc/wireguard/peers", interfaceName)
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
			logger.Warnf("[wireguard] failed to set Address: %v", err)
		}
	}
	if ifaceCfg.ListenPort > 0 {
		if _, err := ifSec.NewKey("ListenPort", fmt.Sprintf("%d", ifaceCfg.ListenPort)); err != nil {
			logger.Warnf("[wireguard] failed to set ListenPort: %v", err)
		}
	}
	if peer.PrivateKey == "" {
		return "", fmt.Errorf("peer private key is empty")
	}
	if _, err := ifSec.NewKey("PrivateKey", peer.PrivateKey); err != nil {
		logger.Warnf("[wireguard] failed to set PrivateKey: %v", err)
	}
	if len(ifaceCfg.DNS) > 0 {
		if _, err := ifSec.NewKey("DNS", strings.Join(ifaceCfg.DNS, ",")); err != nil {
			logger.Warnf("[wireguard] failed to set DNS: %v", err)
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
		logger.Warnf("[wireguard] failed to set PublicKey in peer section: %v", err)
	}
	if _, err := peerSec.NewKey("AllowedIPs", "0.0.0.0/0, ::/0"); err != nil {
		logger.Warnf("[wireguard] failed to set AllowedIPs in peer section: %v", err)
	}

	// Get public IP
	publicIP, err := getPublicIP()
	if err != nil {
		return "", fmt.Errorf("failed to get public IP: %w", err)
	}
	if _, err := peerSec.NewKey("Endpoint", publicIP); err != nil {
		logger.Warnf("[wireguard] failed to set Endpoint in peer section: %v", err)
	}

	if peer.PresharedKey != "" {
		if _, err := peerSec.NewKey("PresharedKey", peer.PresharedKey); err != nil {
			logger.Warnf("[wireguard] failed to set PresharedKey: %v", err)
		}
	}
	if peer.PersistentKeepalive > 0 {
		if _, err := peerSec.NewKey("PersistentKeepalive", fmt.Sprintf("%d", peer.PersistentKeepalive)); err != nil {
			logger.Warnf("[wireguard] failed to set PersistentKeepalive: %v", err)
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
	peerDir := filepath.Join("/etc/wireguard/peers", interfaceName)
	files, err := filepath.Glob(filepath.Join(peerDir, "*.conf"))
	if err != nil {
		return nil, fmt.Errorf("failed to list peer configs: %w", err)
	}

	var peers []utils.PeerConfig

	for _, file := range files {
		iniFile, err := ini.Load(file)
		if err != nil {
			logger.Warnf("[wireguard] Failed to parse peer config %s: %v", file, err)
			continue
		}
		ifSec := iniFile.Section("Interface")
		pc := utils.PeerConfig{
			PublicKey:    ifSec.Key("PublicKey").String(),
			PresharedKey: ifSec.Key("PresharedKey").String(),
			Endpoint:     ifSec.Key("Endpoint").String(),
			Name:         ifSec.Key("Name").String(),
		}
		pc.AllowedIPs = filterEmpty(strings.Split(ifSec.Key("AllowedIPs").String(), ","))
		pc.PersistentKeepalive, _ = ifSec.Key("PersistentKeepalive").Int()
		// If Name is empty, get from filename (e.g., Peer3.conf → Peer3)
		if pc.Name == "" {
			pc.Name = peerNameFromPath(file)
		}
		peers = append(peers, pc)
	}

	return peers, nil
}

// --- Handler Implementations ---

func ListInterfaces(args []string) (any, error) {
	logger.Debugf("[wireguard] Listing interfaces")
	files, err := filepath.Glob("/etc/wireguard/*.conf")
	if err != nil {
		logger.Errorf("[wireguard] Failed to list interfaces: %v", err)
		return nil, err
	}

	var uiIfaces []WireGuardInterfaceUI

	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".conf")
		cfg, err := ParseWireGuardConfig(configPath(name))
		if err != nil {
			logger.Warnf("[wireguard] Failed to parse config for %s: %v", name, err)
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
	logger.Infof("[wireguard] Found interfaces: %d", len(uiIfaces))
	return uiIfaces, nil
}

func AddInterface(args []string) (any, error) {
	if len(args) < 4 {
		logger.Warnf("[wireguard] add_interface: missing args")
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

	for _, peer := range peers {
		if _, err := ExportPeerConfigToDisk(name, peer, cfg); err != nil {
			logger.Warnf("[wireguard] Failed to export client config for peer %s: %v", peer.PublicKey, err)
		} else {
			logger.Infof("[wireguard] Exported client config for peer %s", peer.PublicKey)
		}
	}

	if err := WriteWireGuardConfigWithPostUpDown(configPath(name), cfg, egressNic); err != nil {
		logger.Errorf("[wireguard] Failed to write config for %s: %v", name, err)
		return nil, err
	}

	if _, err := UpInterface([]string{name}); err != nil {
		return nil, err // logging already done in UpInterface
	}

	logger.Infof("[wireguard] Interface %s created and brought up", name)

	return map[string]any{
		"status":      "created",
		"private_key": privateKey,
		"public_key":  publicKey,
		"peers":       peers,
	}, nil

}

func RemoveInterface(args []string) (any, error) {
	if len(args) < 1 {
		logger.Warnf("[wireguard] remove_interface: missing name argument")
		return nil, fmt.Errorf("usage: remove_interface <name>")
	}
	name := args[0]
	logger.Infof("[wireguard] Removing interface: %s", name)
	if _, err := DownInterface([]string{name}); err != nil {
		logger.Warnf("[wireguard] Failed to bring down %s: %v", name, err)
		// Not returning, since removal should proceed regardless
	}

	if err := os.Remove(configPath(name)); err != nil {
		logger.Errorf("[wireguard] Failed to remove config for %s: %v", name, err)
		return nil, err
	}
	// Remove all peer configs for this interface
	peerDir := filepath.Join("/etc/wireguard/peers", name)
	if err := os.RemoveAll(peerDir); err == nil {
		logger.Infof("[wireguard] Removed all peer configs in %s", peerDir)
	} else if !os.IsNotExist(err) {
		logger.Warnf("[wireguard] Could not remove peer config dir %s: %v", peerDir, err)
	}
	logger.Infof("[wireguard] Interface %s removed", name)
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
	if len(args) < 2 {
		logger.Warnf("[wireguard] add_peer: missing args")
		return nil, fmt.Errorf("usage: add_peer <interface> <publicKey> [allowedIPs] [endpoint] [presharedKey] [persistentKeepalive] [name]")
	}
	name := args[0]
	pub := args[1]
	logger.Infof("[wireguard] Adding peer to %s: %s", name, pub)
	allowedIPs := []string{}
	if len(args) > 2 && args[2] != "" {
		allowedIPs = filterEmpty(strings.Split(args[2], ","))
	}
	endpoint := ""
	if len(args) > 3 {
		endpoint = args[3]
	}
	preshared := ""
	if len(args) > 4 {
		preshared = args[4]
	}
	keepalive := 0
	if len(args) > 5 && args[5] != "" {
		keepalive, _ = strconv.Atoi(args[5])
	}
	peerName := ""
	if len(args) > 6 {
		peerName = args[6]
	}

	peer := utils.PeerConfig{
		PublicKey:           pub,
		AllowedIPs:          allowedIPs,
		Endpoint:            endpoint,
		PresharedKey:        preshared,
		PersistentKeepalive: keepalive,
		Name:                peerName, // Only set if provided
	}

	cfg, err := ParseWireGuardConfig(configPath(name))
	if err != nil {
		logger.Errorf("[wireguard] Failed to read config for %s: %v", name, err)
		return nil, err
	}
	AddOrUpdatePeerInConfig(&cfg, peer)
	if err := WriteWireGuardConfig(configPath(name), cfg); err != nil {
		logger.Errorf("[wireguard] Failed to write config for %s: %v", name, err)
		return nil, err
	}

	// Apply to running interface (optional)
	pubKey, err := wgtypes.ParseKey(pub)
	if err == nil {
		client, _ := wgctrl.New()
		defer func() {
			if cerr := client.Close(); cerr != nil {
				logger.Warnf("failed to close wgctrl client: %v", cerr)
			}
		}()
		_ = client.ConfigureDevice(name, wgtypes.Config{
			Peers: []wgtypes.PeerConfig{
				{
					PublicKey: pubKey,
				},
			},
		})
	}

	// Export client config file for this peer
	if _, err := ExportPeerConfigToDisk(name, peer, cfg); err != nil {
		logger.Warnf("[wireguard] Failed to export client config for peer %s on %s: %v", pub, name, err)
	} else {
		logger.Infof("[wireguard] Exported client config for peer %s on %s", pub, name)
	}
	return "added", nil
}

func RemovePeer(args []string) (any, error) {
	if len(args) < 2 {
		logger.Warnf("[wireguard] remove_peer: missing args")
		return nil, fmt.Errorf("usage: remove_peer <interface> <publicKey>")
	}
	name := args[0]
	pub := args[1]
	logger.Infof("[wireguard] Removing peer from %s: %s", name, pub)
	cfg, err := ParseWireGuardConfig(configPath(name))
	if err != nil {
		logger.Errorf("[wireguard] Failed to read config for %s: %v", name, err)
		return nil, err
	}
	if !RemovePeerFromConfig(&cfg, pub) {
		logger.Warnf("[wireguard] Peer %s not found in %s", pub, name)
		return nil, fmt.Errorf("peer not found")
	}
	if err := WriteWireGuardConfig(configPath(name), cfg); err != nil {
		logger.Errorf("[wireguard] Failed to write config for %s: %v", name, err)
		return nil, err
	}
	pubKey, err := wgtypes.ParseKey(pub)
	if err == nil {
		client, err := wgctrl.New()
		if err != nil {
			logger.Warnf("[wireguard] failed to create wgctrl client: %v", err)
		} else {
			defer func() {
				if cerr := client.Close(); cerr != nil {
					logger.Warnf("[wireguard] failed to close wgctrl client: %v", cerr)
				}
			}()
			if err := client.ConfigureDevice(name, wgtypes.Config{
				Peers: []wgtypes.PeerConfig{{
					PublicKey: pubKey,
					Remove:    true,
				}},
			}); err != nil {
				logger.Warnf("[wireguard] failed to configure device (remove peer): %v", err)
			}
		}
	}

	// Remove exported client config by matching public key
	peerDir := filepath.Join("/etc/wireguard/peers", name)
	removeExportedPeerConfig(peerDir, pub)

	logger.Infof("[wireguard] Peer %s removed from interface %s", pub, name)
	return "removed", nil
}

func GetKeys(args []string) (any, error) {
	if len(args) < 1 {
		logger.Warnf("[wireguard] get_keys: missing interface argument")
		return nil, fmt.Errorf("usage: get_keys <interface>")
	}
	name := args[0]
	logger.Infof("[wireguard] Getting keys for interface: %s", name)
	cfg, err := ParseWireGuardConfig(configPath(name))
	if err != nil {
		logger.Errorf("[wireguard] Failed to read config for %s: %v", name, err)
		return nil, err
	}
	priv := cfg.PrivateKey
	key, _ := wgtypes.ParseKey(priv)
	logger.Infof("[wireguard] Keys for %s retrieved", name)
	return map[string]string{
		"private_key": priv,
		"public_key":  key.PublicKey().String(),
	}, nil
}

func WriteWireGuardConfigWithPostUpDown(path string, cfg InterfaceConfig, egressNic string) error {
	iniFile := ini.Empty()
	ifSec, err := iniFile.NewSection("Interface")
	if err != nil {
		logger.Warnf("[wireguard] failed to create [Interface] section: %v", err)
	}
	if _, err := ifSec.NewKey("PrivateKey", cfg.PrivateKey); err != nil {
		logger.Warnf("[wireguard] failed to set PrivateKey: %v", err)
	}
	if len(cfg.Address) > 0 {
		if _, err := ifSec.NewKey("Address", strings.Join(cfg.Address, ",")); err != nil {
			logger.Warnf("[wireguard] failed to set Address: %v", err)
		}
	}
	if cfg.ListenPort > 0 {
		if _, err := ifSec.NewKey("ListenPort", fmt.Sprint(cfg.ListenPort)); err != nil {
			logger.Warnf("[wireguard] failed to set ListenPort: %v", err)
		}
	}
	if len(cfg.DNS) > 0 {
		if _, err := ifSec.NewKey("DNS", strings.Join(cfg.DNS, ",")); err != nil {
			logger.Warnf("[wireguard] failed to set DNS: %v", err)
		}
	}
	if cfg.MTU > 0 {
		if _, err := ifSec.NewKey("MTU", fmt.Sprint(cfg.MTU)); err != nil {
			logger.Warnf("[wireguard] failed to set MTU: %v", err)
		}
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
		logger.Warnf("[wireguard] failed to set PostUp: %v", err)
	}
	if _, err := ifSec.NewKey("PostDown", postDown); err != nil {
		logger.Warnf("[wireguard] failed to set PostDown: %v", err)
	}

	for _, peer := range cfg.Peers {
		psec, err := iniFile.NewSection("Peer")
		if err != nil {
			logger.Warnf("[wireguard] failed to create [Peer] section: %v", err)
			continue
		}
		if _, err := psec.NewKey("PublicKey", peer.PublicKey); err != nil {
			logger.Warnf("[wireguard] failed to set PublicKey: %v", err)
		}
		if peer.PresharedKey != "" {
			if _, err := psec.NewKey("PresharedKey", peer.PresharedKey); err != nil {
				logger.Warnf("[wireguard] failed to set PresharedKey: %v", err)
			}
		}
		if len(peer.AllowedIPs) > 0 {
			if _, err := psec.NewKey("AllowedIPs", strings.Join(peer.AllowedIPs, ",")); err != nil {
				logger.Warnf("[wireguard] failed to set AllowedIPs: %v", err)
			}
		}
		if peer.Endpoint != "" {
			if _, err := psec.NewKey("Endpoint", peer.Endpoint); err != nil {
				logger.Warnf("[wireguard] failed to set Endpoint: %v", err)
			}
		}
		if peer.PersistentKeepalive > 0 {
			if _, err := psec.NewKey("PersistentKeepalive", fmt.Sprint(peer.PersistentKeepalive)); err != nil {
				logger.Warnf("[wireguard] failed to set PersistentKeepalive: %v", err)
			}
		}
	}

	return iniFile.SaveTo(path)
}

func UpInterface(args []string) (any, error) {
	if len(args) < 1 {
		logger.Warnf("[wireguard] up_interface: missing name argument")
		return nil, fmt.Errorf("usage: up_interface <name>")
	}
	name := args[0]

	cmd := exec.Command("wg-quick", "up", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf("[wireguard] Failed to bring up %s: %v (%s)", name, err, string(out))
		return nil, fmt.Errorf("failed to bring up interface: %v (%s)", err, string(out))
	}
	if len(out) > 0 {
		logger.Debugf("[wireguard] wg-quick up output for %s: %s", name, string(out))
	}

	logger.Infof("[wireguard] Interface %s brought up", name)

	return map[string]any{
		"status": "on",
		"output": string(out),
	}, nil
}

func DownInterface(args []string) (any, error) {
	if len(args) < 1 {
		logger.Warnf("[wireguard] down_interface: missing name argument")
		return nil, fmt.Errorf("usage: down_interface <name>")
	}
	name := args[0]

	cmd := exec.Command("wg-quick", "down", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf("[wireguard] Failed to bring down %s: %v (%s)", name, err, string(out))
		return nil, fmt.Errorf("failed to bring down interface: %v (%s)", err, string(out))
	}
	if len(out) > 0 {
		logger.Debugf("[wireguard] wg-quick down output for %s: %s", name, string(out))
	}

	logger.Infof("[wireguard] Interface %s brought down", name)

	return map[string]any{
		"status": "off",
		"output": string(out),
	}, nil
}
