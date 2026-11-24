package wireguard

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// applyTunnelMove applies the request's address and network card to cfg,
// after the rest of the update, and returns the exported peer files a new
// address renumbers, which are written with cfg. The NAT lines LinuxIO wrote
// follow both: their card is the new card and their source the new address.
func applyTunnelMove(ctx context.Context, cfg *WireGuardConfig, req apischema.WireGuardUpdateInterfaceRequest) ([]peerFile, error) {
	egress, source, hasNAT := natRules(*cfg)
	newEgress, newSource := egress, source
	if req.EgressNic != nil && strings.TrimSpace(*req.EgressNic) != egress {
		nic := strings.TrimSpace(*req.EgressNic)
		if err := validateInterfaceName(nic); err != nil {
			return nil, fmt.Errorf("%w: invalid network card: %w", bridgeipc.ErrInvalidArgs, err)
		}
		if !hasNAT {
			return nil, fmt.Errorf("%w: %s has no NAT lines written by LinuxIO to move to %s: edit its PostUp and PostDown lines", bridgeipc.ErrConflict, req.Name, nic)
		}
		newEgress = nic
	}

	var files []peerFile
	if req.Address != nil {
		address, to, err := parseServerAddress(*req.Address)
		if err != nil {
			return nil, err
		}
		if len(cfg.Address) == 0 || address != cfg.Address[0] {
			if other := interfaceUsingSubnet(req.Name, address); other != "" {
				return nil, fmt.Errorf("%w: %s overlaps the subnet of %s", bridgeipc.ErrConflict, address, other)
			}
			peers, err := loadExportedPeers(ctx, req.Name)
			if err != nil {
				return nil, err
			}
			if files, err = renumber(cfg, peers, address, to); err != nil {
				return nil, err
			}
			newSource = address
		}
	}

	if hasNAT && (newEgress != egress || newSource != source) {
		replaceNATHooks(cfg, egress, source, newEgress, newSource)
	}
	return files, nil
}

// parseServerAddress reads a new tunnel address: an IPv4 subnet LinuxIO
// numbers peers in (/24 to /30), with the server on its first host. The
// subnet's own address stands for that host, as at create.
func parseServerAddress(value string) (string, *ipManager, error) {
	value = strings.TrimSpace(value)
	if _, _, err := net.ParseCIDR(value); err != nil {
		return "", nil, fmt.Errorf("%w: address %q is not a CIDR", bridgeipc.ErrInvalidArgs, value)
	}
	address := normalizeAddresses([]string{value})[0]
	to, err := newIPManager(address)
	if err != nil {
		return "", nil, fmt.Errorf("%w: address %s: %w", bridgeipc.ErrInvalidArgs, address, err)
	}
	if want := to.makeIP(to.serverHost); to.extractHostOffset(address) != to.serverHost {
		return "", nil, fmt.Errorf("%w: the server takes the first address of its subnet: use %s/%d", bridgeipc.ErrInvalidArgs, strings.TrimSuffix(want, "/32"), to.maskBits)
	}
	return address, to, nil
}

// interfaceUsingSubnet returns the name of another interface config with an
// address whose subnet overlaps address's, or "".
func interfaceUsingSubnet(self, address string) string {
	_, want, err := net.ParseCIDR(address)
	if err != nil {
		return ""
	}
	files, _ := filepath.Glob(filepath.Join(wgConfigDir, "*"+configExt))
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), configExt)
		if name == self {
			continue
		}
		other, err := ParseWireGuardConfig(file)
		if err != nil {
			continue
		}
		for _, entry := range other.Address {
			if _, have, err := net.ParseCIDR(entry); err == nil && (have.Contains(want.IP) || want.Contains(have.IP)) {
				return name
			}
		}
	}
	return ""
}

// renumber moves cfg and its exported peers into the subnet of server, the
// new tunnel address. A peer keeps its host number where the new subnet has
// it free and takes the first free one otherwise. The client routes and DNS
// servers that name an address of the old subnet move with it, as do the
// [Peer] sections written by hand; the sections of exported peers are
// rebuilt from their files, which are returned to be written.
func renumber(cfg *WireGuardConfig, peers []PeerInfo, server string, to *ipManager) ([]peerFile, error) {
	if len(cfg.Address) == 0 {
		return nil, fmt.Errorf("%w: the interface has no address to renumber its peers from", bridgeipc.ErrConflict)
	}
	from, err := newIPManager(cfg.Address[0])
	if err != nil {
		return nil, fmt.Errorf("%w: the peers of %s cannot be renumbered: %w", bridgeipc.ErrConflict, cfg.Address[0], err)
	}
	move := subnetMove{from: from, to: to}

	exported := make(map[string]bool, len(peers))
	for _, peer := range peers {
		exported[peer.PublicKey] = true
	}
	// A section with no exported file was written by hand: it moves first,
	// so its host numbers are taken before any peer needs a free one.
	sections := slices.Clone(cfg.Peers)
	var used []PeerConfig
	for i, section := range sections {
		if exported[section.PublicKey] {
			continue
		}
		sections[i].AllowedIPs = move.routes(section.AllowedIPs)
		used = append(used, sections[i])
	}

	files := make([]peerFile, len(peers))
	var homeless []int
	for i, peer := range peers {
		files[i] = peer.peerFile
		moved, ok := move.host(peer.Address)
		if !ok || to.buildUsedIPMap(used)[to.extractHostOffset(moved)] {
			homeless = append(homeless, i)
			continue
		}
		files[i].Address = moved
		used = append(used, PeerConfig{AllowedIPs: []string{moved}})
	}
	for _, i := range homeless {
		address, _, err := to.findNextAvailable(used)
		if err != nil {
			return nil, fmt.Errorf("%w: %s has no room for the %d peers of the interface", bridgeipc.ErrConflict, to.network(), len(peers))
		}
		files[i].Address = address
		used = append(used, PeerConfig{AllowedIPs: []string{address}})
	}

	cfg.Peers = sections
	for i := range files {
		files[i].ClientAllowedIPs = move.routes(files[i].ClientAllowedIPs)
		files[i].DNS = move.hosts(files[i].DNS)
		if !files[i].Enabled {
			continue
		}
		section, err := files[i].serverSection()
		if err != nil {
			return nil, fmt.Errorf("peer %s: %w", files[i].ID, err)
		}
		replacePeerSection(cfg, peers[i].PublicKey, &section)
	}
	cfg.DNS = move.hosts(cfg.DNS)
	cfg.Address = append([]string{server}, cfg.Address[1:]...)
	return files, nil
}

// subnetMove carries an address of an interface's old subnet to the same
// host number in its new one.
type subnetMove struct{ from, to *ipManager }

// host moves one address, bare or as a /32, and reports false for one
// outside the old subnet, its network or broadcast address, or past the new
// subnet's last host.
func (m subnetMove) host(value string) (string, bool) {
	ip, suffix, hasSuffix := strings.Cut(strings.TrimSpace(value), "/")
	if hasSuffix && suffix != "32" {
		return "", false
	}
	offset := m.from.extractHostOffset(ip)
	if offset < m.from.serverHost || offset > m.from.maxHost || offset > m.to.maxHost {
		return "", false
	}
	moved := m.to.makeIP(offset)
	if !hasSuffix {
		moved = strings.TrimSuffix(moved, "/32")
	}
	return moved, true
}

// hosts moves each address of values that host can, and keeps the rest.
func (m subnetMove) hosts(values []string) []string {
	if values == nil {
		return nil
	}
	moved := make([]string, len(values))
	for i, value := range values {
		moved[i] = value
		if to, ok := m.host(value); ok {
			moved[i] = to
		}
	}
	return moved
}

// routes moves routes: the old subnet becomes the new one, and a host route
// inside it moves with its host.
func (m subnetMove) routes(values []string) []string {
	moved := m.hosts(values)
	for i, value := range values {
		if _, network, err := net.ParseCIDR(strings.TrimSpace(value)); err == nil && network.String() == m.from.network() {
			moved[i] = m.to.network()
		}
	}
	return moved
}
