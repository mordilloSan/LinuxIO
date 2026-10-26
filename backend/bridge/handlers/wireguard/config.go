package wireguard

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/ini.v1"
)

const (
	wgQuickInterface = "%i"
	metaKeyHost      = "host"
	metaKeyDNS       = "dns"
)

// interfaceKeys are the [Interface] keys the model carries; any other key is
// kept verbatim in Extra so a rewrite never changes what wg-quick applies.
var interfaceKeys = map[string]bool{
	"PrivateKey": true, "Address": true, "ListenPort": true, "MTU": true,
	"PreUp": true, "PostUp": true, "PreDown": true, "PostDown": true,
}

// loadWGINI reads a WireGuard INI the way wg-quick does: a '#' ends a value
// (`ListenPort = 51820 # main`) while ';' is ordinary text, so a hook such as
// "cmd1; cmd2" survives. Whole-line comments stay for the metadata parser.
func loadWGINI(path string, opts ini.LoadOptions) (*ini.File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if before, _, ok := strings.Cut(line, "#"); ok {
			lines[i] = before
		}
	}
	opts.IgnoreInlineComment = true
	f, err := ini.LoadSources(opts, []byte(strings.Join(lines, "\n")))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return f, nil
}

func ParseWireGuardConfig(path string) (WireGuardConfig, error) {
	var cfg WireGuardConfig

	iniFile, err := loadWGINI(path, ini.LoadOptions{
		AllowNonUniqueSections: true,
		AllowShadows:           true,
	})
	if err != nil {
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
	for _, key := range ifSec.Keys() {
		if interfaceKeys[key.Name()] {
			continue
		}
		for _, value := range key.ValueWithShadows() {
			cfg.Extra = append(cfg.Extra, [2]string{key.Name(), value})
		}
	}

	meta := parseMetadata(ifSec.Comment)
	cfg.Host = meta[metaKeyHost]
	cfg.DNS = parseCSV(meta[metaKeyDNS])

	// Parse Peer sections
	for _, sec := range iniFile.Sections() {
		if !isPeerSection(sec.Name()) {
			continue
		}

		// The server config carries the name only as a comment above [Peer],
		// since wg setconf rejects unknown keys.
		pc := PeerConfig{
			Name:         strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(sec.Comment), "#")),
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
		// Without this, ini wraps a value holding ';' in backticks, which
		// wg-quick's eval runs as command substitution.
		IgnoreInlineComment: true,
	})

	// Create Interface section
	ifSec, err := iniFile.NewSection("Interface")
	if err != nil {
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
	for _, kv := range cfg.Extra {
		setKey(ifSec, kv[0], kv[1])
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
		return fmt.Errorf("save config: %w", err)
	}
	slog.Debug("wrote WireGuard config", "component", "wireguard", "subsystem", "config", "path", path)
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

func configPath(name string) string {
	return filepath.Join(wgConfigDir, name+configExt)
}

func parseCSV(s string) []string {
	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	return result
}

func setKey(section *ini.Section, key, value string) {
	if _, err := section.NewKey(key, value); err != nil {
		slog.Warn("failed to set INI key", "component", "wireguard", "subsystem", "config", "path", key, "error", err)
	}
}

func setKeyIfNotEmpty(section *ini.Section, key, value string) {
	if value != "" {
		setKey(section, key, value)
	}
}

func setKeyIfPositive(section *ini.Section, key string, value int) {
	if value > 0 {
		setKey(section, key, strconv.Itoa(value))
	}
}

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

func isPeerSection(name string) bool {
	return name == "Peer" || strings.HasPrefix(name, "Peer ")
}
