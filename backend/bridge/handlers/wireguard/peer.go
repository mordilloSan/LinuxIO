package wireguard

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gopkg.in/ini.v1"
)

// --- Peer Management ---

func addPeerSection(iniFile *ini.File, peer PeerConfig) error {
	psec, err := iniFile.NewSection("Peer")
	if err != nil {
		return err
	}

	setKey(psec, "PublicKey", peer.PublicKey)
	setKeyIfNotEmpty(psec, "PresharedKey", peer.PresharedKey)
	setKeyIfNotEmpty(psec, "AllowedIPs", strings.Join(peer.AllowedIPs, ","))
	setKeyIfNotEmpty(psec, "Endpoint", peer.Endpoint)
	setKeyIfPositive(psec, "PersistentKeepalive", peer.PersistentKeepalive)

	return nil
}

func ExportPeerConfig(interfaceName string, peer PeerConfig, ifaceCfg InterfaceConfig, publicIP string, peerNumber int, dnsOverride string) (string, error) {
	// Ensure peer directory exists
	peerDir := peerDirPath(interfaceName)
	if err := os.MkdirAll(peerDir, 0o700); err != nil {
		slog.Error("failed to create peer directory", "component", "wireguard", "subsystem", "peer", "interface", interfaceName, "path", peerDir, "error", err)
		return "", fmt.Errorf("create peer dir: %w", err)
	}

	peerPath := filepath.Join(peerDir, fmt.Sprintf("Peer%d.conf", peerNumber))
	iniFile := ini.Empty()

	// Create Interface section for peer
	ifSec, err := iniFile.NewSection("Interface")
	if err != nil {
		slog.Error("failed to create peer interface section", "component", "wireguard", "subsystem", "peer", "interface", interfaceName, "path", peerPath, "error", err)
		return "", fmt.Errorf("create interface section: %w", err)
	}

	// Set peer interface configuration
	if len(peer.AllowedIPs) > 0 {
		setKey(ifSec, "Address", peer.AllowedIPs[0])
	}
	setKeyIfPositive(ifSec, "ListenPort", ifaceCfg.ListenPort)

	if peer.PrivateKey == "" {
		slog.Error("peer private key is empty", "component", "wireguard", "subsystem", "peer", "interface", interfaceName, "path", peerPath)
		return "", fmt.Errorf("peer private key is empty")
	}
	setKey(ifSec, "PrivateKey", peer.PrivateKey)

	// DNS precedence: interface DNS > override > none
	dnsVal := ""
	if len(ifaceCfg.DNS) > 0 {
		dnsVal = strings.Join(ifaceCfg.DNS, ",")
	} else if dnsOverride != "" {
		dnsVal = dnsOverride
	}
	setKeyIfNotEmpty(ifSec, "DNS", dnsVal)

	// Create Peer section (connecting to server)
	peerSec, err := iniFile.NewSection("Peer")
	if err != nil {
		slog.Error("failed to create peer section", "component", "wireguard", "subsystem", "peer", "interface", interfaceName, "path", peerPath, "error", err)
		return "", fmt.Errorf("create peer section: %w", err)
	}

	// Get server public key
	serverKey, err := wgtypes.ParseKey(ifaceCfg.PrivateKey)
	if err != nil {
		slog.Error("failed to parse server private key", "component", "wireguard", "subsystem", "peer", "interface", interfaceName, "error", err)
		return "", fmt.Errorf("parse server key: %w", err)
	}

	setKey(peerSec, "PublicKey", serverKey.PublicKey().String())
	setKey(peerSec, "AllowedIPs", "0.0.0.0/1, 128.0.0.0/1, ::/0")

	// Set endpoint if we have public IP - using simple format
	if publicIP != "" && ifaceCfg.ListenPort > 0 {
		endpoint := fmt.Sprintf("%s:%d", publicIP, ifaceCfg.ListenPort)
		setKey(peerSec, "Endpoint", endpoint)
	}

	setKeyIfNotEmpty(peerSec, "PresharedKey", peer.PresharedKey)
	setKeyIfPositive(peerSec, "PersistentKeepalive", peer.PersistentKeepalive)

	// Save peer config
	if err := iniFile.SaveTo(peerPath); err != nil {
		slog.Error("failed to save peer config", "component", "wireguard", "subsystem", "peer", "interface", interfaceName, "path", peerPath, "error", err)
		return "", fmt.Errorf("save peer config: %w", err)
	}
	slog.Info("wrote peer config", "component", "wireguard", "subsystem", "peer", "interface", interfaceName, "path", peerPath)
	return peerPath, nil
}

func isPeerSection(name string) bool {
	return name == "Peer" || strings.HasPrefix(name, "Peer ")
}
