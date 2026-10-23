package wireguard

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gopkg.in/ini.v1"
)

// peerFile is one exported client config under /etc/wireguard/<iface>/ plus
// the LinuxIO metadata kept as "# linuxio-<key>: <value>" comment lines above
// [Interface]. The file is the single source of truth for a peer; the server
// config's [Peer] section is derived from it with serverSection.
type peerFile struct {
	ID               string // file stem, stable
	Name             string
	Enabled          bool
	ServerAllowedIPs []string // networks behind the peer, added to the server-side AllowedIPs

	PrivateKey string
	Address    string
	DNS        []string
	MTU        int

	ServerPublicKey     string
	PresharedKey        string
	ClientAllowedIPs    []string
	Endpoint            string
	PersistentKeepalive int
}

const (
	metaKeyName             = "name"
	metaKeyEnabled          = "enabled"
	metaKeyServerAllowedIPs = "server-allowed-ips"
)

// metadataLine matches one LinuxIO metadata comment: "# linuxio-<key>: <value>".
var metadataLine = regexp.MustCompile(`^#\s*linuxio-([a-z-]+):\s*(.*)$`)

// parseMetadata reads LinuxIO key/value pairs out of an INI section comment.
func parseMetadata(comment string) map[string]string {
	values := map[string]string{}
	for line := range strings.SplitSeq(comment, "\n") {
		if m := metadataLine.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			values[m[1]] = strings.TrimSpace(m[2])
		}
	}
	return values
}

// formatMetadata renders key/value pairs as the comment block LinuxIO owns.
// Pairs with an empty value are skipped.
func formatMetadata(pairs ...[2]string) string {
	lines := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		if pair[1] == "" {
			continue
		}
		lines = append(lines, "# linuxio-"+pair[0]+": "+pair[1])
	}
	return strings.Join(lines, "\n")
}

func readPeerFile(path string) (peerFile, error) {
	iniFile, err := loadWGINI(path, ini.LoadOptions{})
	if err != nil {
		return peerFile{}, fmt.Errorf("load peer config: %w", err)
	}

	ifSec := iniFile.Section("Interface")
	peerSec := iniFile.Section("Peer")
	p := peerFile{
		ID:                  strings.TrimSuffix(filepath.Base(path), configExt),
		Enabled:             true,
		PrivateKey:          ifSec.Key("PrivateKey").String(),
		Address:             ifSec.Key("Address").String(),
		DNS:                 parseCSV(ifSec.Key("DNS").String()),
		MTU:                 ifSec.Key("MTU").MustInt(0),
		ServerPublicKey:     peerSec.Key("PublicKey").String(),
		PresharedKey:        peerSec.Key("PresharedKey").String(),
		ClientAllowedIPs:    parseCSV(peerSec.Key("AllowedIPs").String()),
		Endpoint:            peerSec.Key("Endpoint").String(),
		PersistentKeepalive: peerSec.Key("PersistentKeepalive").MustInt(0),
	}
	p.Name = p.ID

	meta := parseMetadata(ifSec.Comment)
	if name := meta[metaKeyName]; name != "" {
		p.Name = name
	}
	if enabled, ok := meta[metaKeyEnabled]; ok {
		p.Enabled = enabled != "false"
	}
	p.ServerAllowedIPs = parseCSV(meta[metaKeyServerAllowedIPs])
	return p, nil
}

func writePeerFile(path string, p peerFile) error {
	iniFile := ini.Empty(ini.LoadOptions{IgnoreInlineComment: true})

	ifSec, err := iniFile.NewSection("Interface")
	if err != nil {
		return fmt.Errorf("create interface section: %w", err)
	}
	ifSec.Comment = p.metadataHeader()
	setKey(ifSec, "PrivateKey", p.PrivateKey)
	setKey(ifSec, "Address", p.Address)
	setKeyIfNotEmpty(ifSec, "DNS", strings.Join(p.DNS, ", "))
	setKeyIfPositive(ifSec, "MTU", p.MTU)

	peerSec, err := iniFile.NewSection("Peer")
	if err != nil {
		return fmt.Errorf("create peer section: %w", err)
	}
	setKey(peerSec, "PublicKey", p.ServerPublicKey)
	setKeyIfNotEmpty(peerSec, "PresharedKey", p.PresharedKey)
	setKey(peerSec, "AllowedIPs", strings.Join(p.ClientAllowedIPs, ", "))
	setKeyIfNotEmpty(peerSec, "Endpoint", p.Endpoint)
	setKeyIfPositive(peerSec, "PersistentKeepalive", p.PersistentKeepalive)

	if err := iniFile.SaveTo(path); err != nil {
		return fmt.Errorf("save peer config: %w", err)
	}
	return nil
}

func (p peerFile) metadataHeader() string {
	return formatMetadata(
		[2]string{metaKeyName, p.Name},
		[2]string{metaKeyEnabled, strconv.FormatBool(p.Enabled)},
		[2]string{metaKeyServerAllowedIPs, strings.Join(p.ServerAllowedIPs, ", ")},
	)
}

func (p peerFile) publicKey() (string, error) {
	key, err := wgtypes.ParseKey(p.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("parse peer private key: %w", err)
	}
	return key.PublicKey().String(), nil
}

// stripPeerMetadata removes the LinuxIO header lines from a client config so
// downloads and QR codes carry only what the client needs.
func stripPeerMetadata(raw string) string {
	lines := strings.Split(raw, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if metadataLine.MatchString(strings.TrimSpace(line)) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// serverSection derives the [Peer] section the server config carries for this
// peer: the tunnel address plus any networks behind the peer.
func (p peerFile) serverSection() (PeerConfig, error) {
	pub, err := p.publicKey()
	if err != nil {
		return PeerConfig{}, err
	}
	allowed := make([]string, 0, 1+len(p.ServerAllowedIPs))
	allowed = append(allowed, p.Address)
	allowed = append(allowed, p.ServerAllowedIPs...)
	return PeerConfig{
		PublicKey:           pub,
		PresharedKey:        p.PresharedKey,
		AllowedIPs:          allowed,
		PersistentKeepalive: p.PersistentKeepalive,
		Name:                p.Name,
	}, nil
}

func peerDirPath(iface string) string {
	return filepath.Join(wgConfigDir, iface)
}

func peerConfigPath(iface, peerName string) string {
	return filepath.Join(peerDirPath(iface), peerName+configExt)
}

// fullTunnelAllowedIPs is the default client-side AllowedIPs for new peers.
var fullTunnelAllowedIPs = []string{"0.0.0.0/0", "::/0"}

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
	// The exported file holds the only copy of the client's private key.
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("peer config %s already exists", path)
	}
	if err := writePeerFile(path, file); err != nil {
		slog.Error("failed to save peer config", "component", "wireguard", "subsystem", "peer", "interface", interfaceName, "path", path, "error", err)
		return err
	}
	slog.Info("wrote peer config", "component", "wireguard", "subsystem", "peer", "interface", interfaceName, "path", path)
	return nil
}
