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
	newEgress, err := movedEgress(req, egress, hasNAT)
	if err != nil {
		return nil, err
	}
	files, newSource, err := moveAddress(ctx, cfg, req, source)
	if err != nil {
		return nil, err
	}
	if hasNAT && (newEgress != egress || newSource != source) {
		replaceNATHooks(cfg, egress, source, newEgress, newSource)
	}
	return files, nil
}

// movedEgress returns the card the request moves the NAT lines to, or
// egress when it moves none.
func movedEgress(req apischema.WireGuardUpdateInterfaceRequest, egress string, hasNAT bool) (string, error) {
	if req.EgressNic == nil {
		return egress, nil
	}
	nic := strings.TrimSpace(*req.EgressNic)
	if nic == egress {
		return egress, nil
	}
	if err := validateNICName(nic); err != nil {
		return "", fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
	}
	if !hasNAT {
		return "", fmt.Errorf("%w: %s has no NAT lines written by LinuxIO to move to %s: edit its PostUp and PostDown lines", bridgeipc.ErrConflict, req.Name, nic)
	}
	return nic, nil
}

// moveAddress renumbers cfg into the request's address and returns the peer
// files to write and the NAT source for the address, or source when the
// address stays.
func moveAddress(ctx context.Context, cfg *WireGuardConfig, req apischema.WireGuardUpdateInterfaceRequest, source string) ([]peerFile, string, error) {
	if req.Address == nil {
		return nil, source, nil
	}
	address, to, err := parseServerAddress(*req.Address)
	if err != nil {
		return nil, "", err
	}
	if len(cfg.Address) > 0 && address == cfg.Address[0] {
		return nil, source, nil
	}
	if other := interfaceUsingSubnet(req.Name, address); other != "" {
		return nil, "", fmt.Errorf("%w: %s overlaps the subnet of %s", bridgeipc.ErrConflict, address, other)
	}
	peers, err := loadExportedPeers(ctx, req.Name)
	if err != nil {
		return nil, "", err
	}
	files, err := renumber(cfg, peers, address, to)
	if err != nil {
		return nil, "", err
	}
	return files, address, nil
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

	files, err := movePeerAddresses(peers, move, used)
	if err != nil {
		return nil, err
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

// movePeerAddresses gives each exported peer its address in the new subnet,
// beside the host numbers used already holds. A peer file a move that failed
// part-way already rewrote keeps its address first, as its device may have
// it; then each peer keeps its host number where it is free, and the rest
// take the first free ones.
func movePeerAddresses(peers []PeerInfo, move subnetMove, used []PeerConfig) ([]peerFile, error) {
	files := make([]peerFile, len(peers))
	claim := func(i int, address string) bool {
		if move.to.buildUsedIPMap(used)[move.to.extractHostOffset(address)] {
			return false
		}
		files[i].Address = address
		used = append(used, PeerConfig{AllowedIPs: []string{address}})
		return true
	}
	var pending, homeless []int
	for i, peer := range peers {
		files[i] = peer.peerFile
		_, inOld := move.host(peer.Address)
		if inOld || !move.moved(peer.Address) || !claim(i, peer.Address) {
			pending = append(pending, i)
		}
	}
	for _, i := range pending {
		moved, ok := move.host(peers[i].Address)
		if !ok || !claim(i, moved) {
			homeless = append(homeless, i)
		}
	}
	for _, i := range homeless {
		address, _, err := move.to.findNextAvailable(used)
		if err != nil {
			return nil, fmt.Errorf("%w: %s has no room for the %d peers of the interface", bridgeipc.ErrConflict, move.to.network(), len(peers))
		}
		claim(i, address)
	}
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

// moved reports whether value is already a peer address of the new subnet.
func (m subnetMove) moved(value string) bool {
	_, suffix, _ := strings.Cut(strings.TrimSpace(value), "/")
	offset := m.to.extractHostOffset(value)
	return suffix == "32" && offset >= minHostOffset && offset <= m.to.maxHost
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
