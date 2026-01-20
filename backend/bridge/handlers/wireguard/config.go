package wireguard

import (
	"fmt"
	"strings"

	"gopkg.in/ini.v1"

	"github.com/mordilloSan/go-logger/logger"
)

// --- Config Parsing ---
func ParseWireGuardConfig(path string) (InterfaceConfig, error) {
	var cfg InterfaceConfig

	iniFile, err := ini.LoadSources(ini.LoadOptions{
		AllowNonUniqueSections: true,
	}, path)
	if err != nil {
		logger.Errorf("ParseWireGuardConfig: failed to load %s: %v", path, err)
		return cfg, fmt.Errorf("load config: %w", err)
	}

	// Parse Interface section
	ifSec := iniFile.Section("Interface")
	cfg.PrivateKey = ifSec.Key("PrivateKey").String()
	cfg.Address = parseCSV(ifSec.Key("Address").String())
	cfg.ListenPort, _ = ifSec.Key("ListenPort").Int()
	cfg.DNS = parseCSV(ifSec.Key("DNS").String())
	cfg.MTU, _ = ifSec.Key("MTU").Int()

	// Parse Peer sections
	peerIdx := 1
	for _, sec := range iniFile.Sections() {
		if !isPeerSection(sec.Name()) {
			continue
		}

		pc := PeerConfig{
			PublicKey:    sec.Key("PublicKey").String(),
			PresharedKey: sec.Key("PresharedKey").String(),
			Endpoint:     sec.Key("Endpoint").String(),
			Name:         sec.Key("Name").String(),
			AllowedIPs:   parseCSV(sec.Key("AllowedIPs").String()),
		}

		if pc.Name == "" {
			pc.Name = fmt.Sprintf("peer%d", peerIdx)
		}

		pc.PersistentKeepalive, _ = sec.Key("PersistentKeepalive").Int()
		cfg.Peers = append(cfg.Peers, pc)
		peerIdx++
	}

	return cfg, nil
}

func WriteWireGuardConfig(path string, cfg InterfaceConfig) error {
	iniFile := ini.Empty(ini.LoadOptions{AllowNonUniqueSections: true})

	// Create Interface section
	ifSec, err := iniFile.NewSection("Interface")
	if err != nil {
		logger.Errorf("WriteWireGuardConfig: create interface section failed for %s: %v", path, err)
		return fmt.Errorf("create interface section: %w", err)
	}

	// Set interface keys
	setKeyIfNotEmpty(ifSec, "Address", strings.Join(cfg.Address, ","))
	setKeyIfPositive(ifSec, "ListenPort", cfg.ListenPort)
	setKey(ifSec, "PrivateKey", cfg.PrivateKey)
	setKeyIfNotEmpty(ifSec, "DNS", strings.Join(cfg.DNS, ","))
	setKeyIfPositive(ifSec, "MTU", cfg.MTU)

	// Create Peer sections
	for _, peer := range cfg.Peers {
		if err := addPeerSection(iniFile, peer); err != nil {
			logger.Warnf("WriteWireGuardConfig: failed to add peer %s: %v", peer.PublicKey, err)
			continue
		}
	}

	// Save file
	if err := iniFile.SaveTo(path); err != nil {
		logger.Errorf("WriteWireGuardConfig: save to %s failed: %v", path, err)
		return fmt.Errorf("save config: %w", err)
	}

	logger.Infof("WriteWireGuardConfig: wrote WireGuard config to %s", path)
	return nil
}
