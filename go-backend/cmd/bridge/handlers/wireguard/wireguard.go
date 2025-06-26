package wireguard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"go-backend/cmd/bridge/handlers/types"
	"go-backend/internal/logger"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gopkg.in/ini.v1"
)

// --- Types ---

type PeerConfig struct {
	PublicKey           string   `json:"public_key"`
	PresharedKey        string   `json:"preshared_key"`
	AllowedIPs          []string `json:"allowed_ips"`
	Endpoint            string   `json:"endpoint"`
	PersistentKeepalive int      `json:"persistent_keepalive"`
}

type InterfaceConfig struct {
	PrivateKey string       `json:"private_key"`
	Address    []string     `json:"address"`
	ListenPort int          `json:"listen_port"`
	DNS        []string     `json:"dns"`
	MTU        int          `json:"mtu"`
	Peers      []PeerConfig `json:"peers"`
}

// --- Helpers ---

func configPath(name string) string {
	return filepath.Join("/etc/wireguard", name+".conf")
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
	for _, sec := range iniFile.Sections() {
		if sec.Name() != "Peer" && !strings.HasPrefix(sec.Name(), "Peer ") {
			continue
		}
		pc := PeerConfig{
			PublicKey:    sec.Key("PublicKey").String(),
			PresharedKey: sec.Key("PresharedKey").String(),
			Endpoint:     sec.Key("Endpoint").String(),
		}
		pc.AllowedIPs = filterEmpty(strings.Split(sec.Key("AllowedIPs").String(), ","))
		pc.PersistentKeepalive, _ = sec.Key("PersistentKeepalive").Int()
		cfg.Peers = append(cfg.Peers, pc)
	}
	return cfg, nil
}

func WriteWireGuardConfig(path string, cfg InterfaceConfig) error {
	iniFile := ini.Empty()
	ifSec, _ := iniFile.NewSection("Interface")
	ifSec.NewKey("PrivateKey", cfg.PrivateKey)
	if len(cfg.Address) > 0 {
		ifSec.NewKey("Address", strings.Join(cfg.Address, ","))
	}
	if cfg.ListenPort > 0 {
		ifSec.NewKey("ListenPort", fmt.Sprint(cfg.ListenPort))
	}
	if len(cfg.DNS) > 0 {
		ifSec.NewKey("DNS", strings.Join(cfg.DNS, ","))
	}
	if cfg.MTU > 0 {
		ifSec.NewKey("MTU", fmt.Sprint(cfg.MTU))
	}
	for _, peer := range cfg.Peers {
		psec, _ := iniFile.NewSection("Peer")
		psec.NewKey("PublicKey", peer.PublicKey)
		if peer.PresharedKey != "" {
			psec.NewKey("PresharedKey", peer.PresharedKey)
		}
		if len(peer.AllowedIPs) > 0 {
			psec.NewKey("AllowedIPs", strings.Join(peer.AllowedIPs, ","))
		}
		if peer.Endpoint != "" {
			psec.NewKey("Endpoint", peer.Endpoint)
		}
		if peer.PersistentKeepalive > 0 {
			psec.NewKey("PersistentKeepalive", fmt.Sprint(peer.PersistentKeepalive))
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
	var newPeers []PeerConfig
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

func AddOrUpdatePeerInConfig(cfg *InterfaceConfig, newPeer PeerConfig) {
	for i, p := range cfg.Peers {
		if p.PublicKey == newPeer.PublicKey {
			cfg.Peers[i] = newPeer
			return
		}
	}
	cfg.Peers = append(cfg.Peers, newPeer)
}

func ExportPeerConfigToDisk(interfaceName string, peer PeerConfig, ifaceCfg InterfaceConfig) (string, error) {
	peerDir := filepath.Join("/etc/wireguard/peers", interfaceName)
	if err := os.MkdirAll(peerDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create peer dir: %w", err)
	}
	peerName := peer.PublicKey[:8] // or any identifier you want
	peerPath := filepath.Join(peerDir, peerName+".conf")

	iniFile := ini.Empty()
	ifSec, _ := iniFile.NewSection("Interface")
	if len(peer.AllowedIPs) > 0 {
		ifSec.NewKey("Address", strings.Join(peer.AllowedIPs, ","))
	}
	if len(ifaceCfg.DNS) > 0 {
		ifSec.NewKey("DNS", strings.Join(ifaceCfg.DNS, ","))
	}
	peerSec, _ := iniFile.NewSection("Peer")
	serverKey, _ := wgtypes.ParseKey(ifaceCfg.PrivateKey)
	peerSec.NewKey("PublicKey", serverKey.PublicKey().String())
	if ifaceCfg.ListenPort > 0 {
		peerSec.NewKey("Endpoint", fmt.Sprintf("<server-ip>:%d", ifaceCfg.ListenPort))
	}
	if peer.PresharedKey != "" {
		peerSec.NewKey("PresharedKey", peer.PresharedKey)
	}
	if peer.PersistentKeepalive > 0 {
		peerSec.NewKey("PersistentKeepalive", fmt.Sprintf("%d", peer.PersistentKeepalive))
	}
	peerSec.NewKey("AllowedIPs", "0.0.0.0/0,::/0")

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

// --- Handler Registration ---

func WireguardHandlers() map[string]types.HandlerFunc {
	return map[string]types.HandlerFunc{
		"list_interfaces": func(args []string) (any, error) {
			logger.Infof("[wireguard] Listing interfaces")
			files, err := filepath.Glob("/etc/wireguard/*.conf")
			if err != nil {
				logger.Errorf("[wireguard] Failed to list interfaces: %v", err)
				return nil, err
			}
			var ifaces []map[string]any
			for _, f := range files {
				name := strings.TrimSuffix(filepath.Base(f), ".conf")
				cfg, err := ParseWireGuardConfig(configPath(name))
				if err != nil {
					logger.Warnf("[wireguard] Failed to parse config for %s: %v", name, err)
					continue // skip broken configs
				}
				status := isInterfaceUp(name)
				// Compose a summary object for each interface
				iface := map[string]any{
					"name":        name,
					"address":     cfg.Address,
					"listen_port": cfg.ListenPort,
					"mtu":         cfg.MTU,
					"dns":         cfg.DNS,
					"peers":       len(cfg.Peers),
					"status":      status,
				}
				ifaces = append(ifaces, iface)
			}
			logger.Infof("[wireguard] Found interfaces: %d", len(ifaces))
			return ifaces, nil
		},

		"get_interface": func(args []string) (any, error) {
			if len(args) < 1 {
				logger.Warnf("[wireguard] get_interface: missing name argument")
				return nil, fmt.Errorf("usage: get_interface <name>")
			}
			name := args[0]
			logger.Infof("[wireguard] Getting interface: %s", name)
			cfg, err := ParseWireGuardConfig(configPath(name))
			if err != nil {
				logger.Errorf("[wireguard] Failed to get interface %s: %v", name, err)
				return nil, err
			}
			return cfg, nil
		},

		"add_interface": func(args []string) (any, error) {
			if len(args) < 4 {
				logger.Warnf("[wireguard] add_interface: missing args")
				return nil, fmt.Errorf("usage: add_interface <name> <privateKey> <addresses> <listenPort>")
			}
			name := args[0]
			logger.Infof("[wireguard] Adding interface: %s", name)
			privateKey := args[1]
			address := filterEmpty(strings.Split(args[2], ","))
			listenPort, _ := strconv.Atoi(args[3])
			cfg := InterfaceConfig{
				PrivateKey: privateKey,
				Address:    address,
				ListenPort: listenPort,
			}
			if err := WriteWireGuardConfig(configPath(name), cfg); err != nil {
				logger.Errorf("[wireguard] Failed to write config for %s: %v", name, err)
				return nil, err
			}
			cmd := exec.Command("wg-quick", "up", name)
			if out, err := cmd.CombinedOutput(); err != nil {
				logger.Errorf("[wireguard] Failed to bring up %s: %v (%s)", name, err, string(out))
				return nil, fmt.Errorf("failed to bring up interface: %v (%s)", err, string(out))
			}
			logger.Infof("[wireguard] Interface %s created and brought up", name)
			return "created", nil
		},

		"remove_interface": func(args []string) (any, error) {
			if len(args) < 1 {
				logger.Warnf("[wireguard] remove_interface: missing name argument")
				return nil, fmt.Errorf("usage: remove_interface <name>")
			}
			name := args[0]
			logger.Infof("[wireguard] Removing interface: %s", name)
			cmd := exec.Command("wg-quick", "down", name)
			cmd.CombinedOutput()
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
		},

		"list_peers": func(args []string) (any, error) {
			if len(args) < 1 {
				logger.Warnf("[wireguard] list_peers: missing interface argument")
				return nil, fmt.Errorf("usage: list_peers <interface>")
			}
			name := args[0]
			logger.Infof("[wireguard] Listing peers for interface: %s", name)
			cfg, err := ParseWireGuardConfig(configPath(name))
			if err != nil {
				logger.Errorf("[wireguard] Failed to list peers for %s: %v", name, err)
				return nil, err
			}
			logger.Infof("[wireguard] Peers for %s: %v", name, cfg.Peers)
			return cfg.Peers, nil
		},

		"add_peer": func(args []string) (any, error) {
			if len(args) < 2 {
				logger.Warnf("[wireguard] add_peer: missing args")
				return nil, fmt.Errorf("usage: add_peer <interface> <publicKey> [allowedIPs] [endpoint] [presharedKey] [persistentKeepalive]")
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
			peer := PeerConfig{
				PublicKey:           pub,
				AllowedIPs:          allowedIPs,
				Endpoint:            endpoint,
				PresharedKey:        preshared,
				PersistentKeepalive: keepalive,
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
				defer client.Close()
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
		},

		"remove_peer": func(args []string) (any, error) {
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
				client, _ := wgctrl.New()
				defer client.Close()
				_ = client.ConfigureDevice(name, wgtypes.Config{
					Peers: []wgtypes.PeerConfig{{
						PublicKey: pubKey,
						Remove:    true,
					}},
				})
			}
			// Remove exported client config
			peerDir := filepath.Join("/etc/wireguard/peers", name)
			peerName := pub
			if len(pub) > 8 {
				peerName = pub[:8]
			}
			peerPath := filepath.Join(peerDir, peerName+".conf")
			if err := os.Remove(peerPath); err == nil {
				logger.Infof("[wireguard] Removed exported config %s", peerPath)
			} else if !os.IsNotExist(err) {
				logger.Warnf("[wireguard] Could not remove exported config %s: %v", peerPath, err)
			}

			logger.Infof("[wireguard] Peer %s removed from interface %s", pub, name)
			return "removed", nil
		},

		"get_keys": func(args []string) (any, error) {
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
		},
	}
}
