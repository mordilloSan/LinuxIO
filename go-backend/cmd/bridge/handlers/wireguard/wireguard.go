package wireguard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
