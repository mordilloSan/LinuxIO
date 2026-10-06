package wireguard

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"gopkg.in/ini.v1"
)

const (
	wgQuickInterface = "%i"
	metaKeyHost      = "host"
	metaKeyDNS       = "dns"
)

// --- Config Parsing ---
func ParseWireGuardConfig(path string) (WireGuardConfig, error) {
	var cfg WireGuardConfig

	iniFile, err := ini.LoadSources(ini.LoadOptions{
		AllowNonUniqueSections: true,
		AllowShadows:           true,
	}, path)
	if err != nil {
		slog.Error("failed to load WireGuard config", "component", "wireguard", "subsystem", "config", "path", path, "error", err)
		return cfg, fmt.Errorf("load config: %w", err)
	}

	// Parse Interface section
	ifSec := iniFile.Section("Interface")
	cfg.PrivateKey = ifSec.Key("PrivateKey").String()
	cfg.Address = parseCSV(ifSec.Key("Address").String())
	if listenPort, parseErr := ifSec.Key("ListenPort").Int(); parseErr == nil {
		cfg.ListenPort = listenPort
	}
	if mtu, parseErr := ifSec.Key("MTU").Int(); parseErr == nil {
		cfg.MTU = mtu
	}
	cfg.PreUp = sectionValues(ifSec, "PreUp")
	cfg.PostUp = sectionValues(ifSec, "PostUp")
	cfg.PreDown = sectionValues(ifSec, "PreDown")
	cfg.PostDown = sectionValues(ifSec, "PostDown")

	meta := parseMetadata(ifSec.Comment)
	cfg.Host = meta[metaKeyHost]
	cfg.DNS = parseCSV(meta[metaKeyDNS])

	// Parse Peer sections
	for _, sec := range iniFile.Sections() {
		if !isPeerSection(sec.Name()) {
			continue
		}

		// Peer names live in the exported peer file; the server config only
		// carries them as a comment, since wg setconf rejects unknown keys.
		pc := PeerConfig{
			PublicKey:    sec.Key("PublicKey").String(),
			PresharedKey: sec.Key("PresharedKey").String(),
			Endpoint:     sec.Key("Endpoint").String(),
			AllowedIPs:   parseCSV(sec.Key("AllowedIPs").String()),
		}

		if keepalive, parseErr := sec.Key("PersistentKeepalive").Int(); parseErr == nil {
			pc.PersistentKeepalive = keepalive
		}
		cfg.Peers = append(cfg.Peers, pc)
	}

	return cfg, nil
}

func WriteWireGuardConfig(path string, cfg WireGuardConfig) error {
	iniFile := ini.Empty(ini.LoadOptions{
		AllowNonUniqueSections: true,
		AllowShadows:           true,
	})

	// Create Interface section
	ifSec, err := iniFile.NewSection("Interface")
	if err != nil {
		slog.Error("failed to create WireGuard interface section", "component", "wireguard", "subsystem", "config", "path", path, "error", err)
		return fmt.Errorf("create interface section: %w", err)
	}

	ifSec.Comment = formatMetadata(
		[2]string{metaKeyHost, cfg.Host},
		[2]string{metaKeyDNS, strings.Join(cfg.DNS, ", ")},
	)

	// Set interface keys
	setKeyIfNotEmpty(ifSec, "Address", strings.Join(cfg.Address, ","))
	setKeyIfPositive(ifSec, "ListenPort", cfg.ListenPort)
	setKey(ifSec, "PrivateKey", cfg.PrivateKey)
	setKeyIfPositive(ifSec, "MTU", cfg.MTU)
	for _, kind := range []struct {
		key   string
		hooks []string
	}{{"PreUp", cfg.PreUp}, {"PostUp", cfg.PostUp}, {"PreDown", cfg.PreDown}, {"PostDown", cfg.PostDown}} {
		for _, hook := range kind.hooks {
			setKeyIfNotEmpty(ifSec, kind.key, hook)
		}
	}

	// Create Peer sections
	for _, peer := range cfg.Peers {
		if err := addPeerSection(iniFile, peer); err != nil {
			slog.Warn("failed to add WireGuard peer section", "component", "wireguard", "subsystem", "config", "path", path, "error", err, "peer", peer.PublicKey)
			continue
		}
	}

	// Save file
	if err := iniFile.SaveTo(path); err != nil {
		slog.Error("failed to save WireGuard config", "component", "wireguard", "subsystem", "config", "path", path, "error", err)
		return fmt.Errorf("save config: %w", err)
	}
	slog.Info("wrote WireGuard config", "component", "wireguard", "subsystem", "config", "path", path)
	return nil
}

func sectionValues(section *ini.Section, name string) []string {
	key, err := section.GetKey(name)
	if err != nil {
		return nil
	}

	values := key.ValueWithShadows()
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
}

func addNATHooks(cfg *WireGuardConfig, egressNic, subnet string) bool {
	changed := false
	for _, hook := range natPostUpHooks(egressNic, subnet) {
		if !slices.Contains(cfg.PostUp, hook) {
			cfg.PostUp = append(cfg.PostUp, hook)
			changed = true
		}
	}
	for _, hook := range natPostDownHooks(egressNic, subnet) {
		if !slices.Contains(cfg.PostDown, hook) {
			cfg.PostDown = append(cfg.PostDown, hook)
			changed = true
		}
	}
	return changed
}

func natPostUpHooks(egressNic, subnet string) []string {
	iface := shellArg(wgQuickInterface)
	egress := shellArg(egressNic)
	source := shellArg(subnet)

	return []string{
		"sysctl -w net.ipv4.ip_forward=1",
		fmt.Sprintf("iptables -C FORWARD -i %s -o %s -j ACCEPT || iptables -I FORWARD 1 -i %s -o %s -j ACCEPT", iface, egress, iface, egress),
		fmt.Sprintf("iptables -C FORWARD -o %s -i %s -m state --state RELATED,ESTABLISHED -j ACCEPT || iptables -I FORWARD 1 -o %s -i %s -m state --state RELATED,ESTABLISHED -j ACCEPT", iface, egress, iface, egress),
		fmt.Sprintf("iptables -t nat -C POSTROUTING -o %s -s %s -j MASQUERADE || iptables -t nat -A POSTROUTING -o %s -s %s -j MASQUERADE", egress, source, egress, source),
	}
}

func natPostDownHooks(egressNic, subnet string) []string {
	iface := shellArg(wgQuickInterface)
	egress := shellArg(egressNic)
	source := shellArg(subnet)

	return []string{
		fmt.Sprintf("iptables -D FORWARD -i %s -o %s -j ACCEPT || true", iface, egress),
		fmt.Sprintf("iptables -D FORWARD -o %s -i %s -m state --state RELATED,ESTABLISHED -j ACCEPT || true", iface, egress),
		fmt.Sprintf("iptables -t nat -D POSTROUTING -o %s -s %s -j MASQUERADE || true", egress, source),
	}
}

// replacePeerSection drops the [Peer] section whose public key is oldPublicKey
// and appends section when it is non-nil. It reports whether a section was
// dropped.
func replacePeerSection(cfg *WireGuardConfig, oldPublicKey string, section *PeerConfig) bool {
	kept := make([]PeerConfig, 0, len(cfg.Peers)+1)
	found := false
	for _, peer := range cfg.Peers {
		if peer.PublicKey == oldPublicKey {
			found = true
			continue
		}
		kept = append(kept, peer)
	}
	if section != nil {
		kept = append(kept, *section)
	}
	cfg.Peers = kept
	return found
}

func shellArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
