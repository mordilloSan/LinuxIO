package wireguard

import (
	"fmt"
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
	iniFile, err := ini.Load(path)
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
	iniFile := ini.Empty()

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
