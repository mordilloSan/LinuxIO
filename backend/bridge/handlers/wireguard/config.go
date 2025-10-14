package wireguard

import (
	"fmt"
	"strings"

	"gopkg.in/ini.v1"

	"github.com/mordilloSan/LinuxIO/backend/common/logger"
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
	return writeConfig(path, cfg, "", false)
}

func WriteWireGuardConfigWithPostUpDown(path string, cfg InterfaceConfig, egressNic string) error {
	return writeConfig(path, cfg, egressNic, true)
}

// Unified config writer
func writeConfig(path string, cfg InterfaceConfig, egressNic string, includePostUpDown bool) error {
	iniFile := ini.Empty(ini.LoadOptions{AllowNonUniqueSections: true})

	// Create Interface section
	ifSec, err := iniFile.NewSection("Interface")
	if err != nil {
		logger.Errorf("writeConfig: create interface section failed for %s: %v", path, err)
		return fmt.Errorf("create interface section: %w", err)
	}

	// Set interface keys
	setKeyIfNotEmpty(ifSec, "Address", strings.Join(cfg.Address, ","))
	setKeyIfPositive(ifSec, "ListenPort", cfg.ListenPort)
	setKey(ifSec, "PrivateKey", cfg.PrivateKey)
	setKeyIfNotEmpty(ifSec, "DNS", strings.Join(cfg.DNS, ","))
	setKeyIfPositive(ifSec, "MTU", cfg.MTU)

	// Add PostUp/PostDown for NAT if requested
	if includePostUpDown && egressNic != "" {
		// Get the subnet from the first address
		subnet := cfg.Address[0]

		postUp := "sysctl -w net.ipv4.ip_forward=1; "
		postUp += fmt.Sprintf("iptables -I FORWARD 1 -i %%i -o %s -j ACCEPT; ", egressNic)
		postUp += fmt.Sprintf("iptables -I FORWARD 1 -o %%i -i %s -m state --state RELATED,ESTABLISHED -j ACCEPT; ", egressNic)
		postUp += fmt.Sprintf("iptables -t nat -A POSTROUTING -o %s -s %s -j MASQUERADE", egressNic, subnet)

		postDown := fmt.Sprintf("iptables -D FORWARD -i %%i -o %s -j ACCEPT; ", egressNic)
		postDown += fmt.Sprintf("iptables -D FORWARD -o %%i -i %s -m state --state RELATED,ESTABLISHED -j ACCEPT; ", egressNic)
		postDown += fmt.Sprintf("iptables -t nat -D POSTROUTING -o %s -s %s -j MASQUERADE", egressNic, subnet)

		setKey(ifSec, "PostUp", postUp)
		setKey(ifSec, "PostDown", postDown)
	}

	// Create Peer sections
	for _, peer := range cfg.Peers {
		if err := addPeerSection(iniFile, peer); err != nil {
			logger.Warnf("writeConfig: failed to add peer %s: %v", peer.PublicKey, err)
			continue
		}
	}

	// Save file
	if err := iniFile.SaveTo(path); err != nil {
		logger.Errorf("writeConfig: save to %s failed: %v", path, err)
		return fmt.Errorf("save config: %w", err)
	}

	// Remove backticks if PostUp/PostDown were added
	if includePostUpDown {
		if err := cleanBackticks(path); err != nil {
			logger.Warnf("writeConfig: cleanBackticks failed for %s: %v", path, err)
			return err
		}
	}

	logger.Infof("writeConfig: wrote WireGuard config to %s", path)
	return nil
}
