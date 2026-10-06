package wireguard

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gopkg.in/ini.v1"
)

// fullTunnelAllowedIPs is the default client-side AllowedIPs for new peers.
var fullTunnelAllowedIPs = []string{"0.0.0.0/0", "::/0"}

// --- Peer Management ---

func addPeerSection(iniFile *ini.File, peer PeerConfig) error {
	psec, err := iniFile.NewSection("Peer")
	if err != nil {
		return err
	}
	if peer.Name != "" {
		psec.Comment = "# " + peer.Name
	}

	setKey(psec, "PublicKey", peer.PublicKey)
	setKeyIfNotEmpty(psec, "PresharedKey", peer.PresharedKey)
	setKeyIfNotEmpty(psec, "AllowedIPs", strings.Join(peer.AllowedIPs, ","))
	setKeyIfNotEmpty(psec, "Endpoint", peer.Endpoint)
	setKeyIfPositive(psec, "PersistentKeepalive", peer.PersistentKeepalive)

	return nil
}

// buildPeerFile turns a generated peer into the client config LinuxIO exports
// for it. DNS precedence: interface DNS, then the egress gateway, then none.
func buildPeerFile(id, name string, peer PeerConfig, ifaceCfg WireGuardConfig, publicIP, gatewayDNS string) (peerFile, error) {
	if peer.PrivateKey == "" {
		return peerFile{}, fmt.Errorf("peer private key is empty")
	}
	if len(peer.AllowedIPs) == 0 {
		return peerFile{}, fmt.Errorf("peer has no allowed IPs")
	}
	serverKey, err := wgtypes.ParseKey(ifaceCfg.PrivateKey)
	if err != nil {
		return peerFile{}, fmt.Errorf("parse server key: %w", err)
	}

	file := peerFile{
		ID:                  id,
		Name:                strings.TrimSpace(name),
		Enabled:             true,
		PrivateKey:          peer.PrivateKey,
		Address:             peer.AllowedIPs[0],
		DNS:                 ifaceCfg.DNS,
		ServerPublicKey:     serverKey.PublicKey().String(),
		PresharedKey:        peer.PresharedKey,
		ClientAllowedIPs:    fullTunnelAllowedIPs,
		PersistentKeepalive: peer.PersistentKeepalive,
	}
	if file.Name == "" {
		file.Name = id
	}
	if len(file.DNS) == 0 && gatewayDNS != "" {
		file.DNS = []string{gatewayDNS}
	}
	if publicIP != "" && ifaceCfg.ListenPort > 0 {
		file.Endpoint = net.JoinHostPort(publicIP, strconv.Itoa(ifaceCfg.ListenPort))
	}
	return file, nil
}

func exportPeerFile(interfaceName string, file peerFile) error {
	peerDir := peerDirPath(interfaceName)
	if err := os.MkdirAll(peerDir, 0o700); err != nil {
		slog.Error("failed to create peer directory", "component", "wireguard", "subsystem", "peer", "interface", interfaceName, "path", peerDir, "error", err)
		return fmt.Errorf("create peer dir: %w", err)
	}
	path := peerConfigPath(interfaceName, file.ID)
	if err := writePeerFile(path, file); err != nil {
		slog.Error("failed to save peer config", "component", "wireguard", "subsystem", "peer", "interface", interfaceName, "path", path, "error", err)
		return err
	}
	slog.Info("wrote peer config", "component", "wireguard", "subsystem", "peer", "interface", interfaceName, "path", path)
	return nil
}

func isPeerSection(name string) bool {
	return name == "Peer" || strings.HasPrefix(name, "Peer ")
}
