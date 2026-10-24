package wireguard

import (
	"fmt"
	"log/slog"
	"net"
	"strings"
)

type ipManager struct {
	netBase    net.IP
	serverHost int
	maskBits   int // subnet mask size (e.g., 24 for /24)
	maxHost    int // maximum host number based on mask
}

func newIPManager(serverCIDR string) (*ipManager, error) {
	ip, ipNet, err := net.ParseCIDR(strings.TrimSpace(serverCIDR))
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", serverCIDR, err)
	}

	// Ensure IPv4 only
	v4 := ip.To4()
	if v4 == nil {
		return nil, fmt.Errorf("IPv6 addresses are not supported: %s", serverCIDR)
	}

	maskBits, totalBits := ipNet.Mask.Size()
	if totalBits != 32 {
		return nil, fmt.Errorf("invalid IPv4 mask for %s", serverCIDR)
	}

	// Only support subnets within a /24 boundary (mask >= 24)
	// This simplifies IP math to last-octet operations
	if maskBits < 24 {
		return nil, fmt.Errorf("subnet too large: max supported is /24, got /%d", maskBits)
	}

	// Calculate host range based on subnet mask
	// For /24: hostBits=8, maxHost=254 (2^8-2)
	// For /28: hostBits=4, maxHost=14 (2^4-2)
	hostBits := 32 - maskBits
	if hostBits < 2 {
		return nil, fmt.Errorf("subnet too small: need at least /30, got /%d", maskBits)
	}
	maxHost := (1 << hostBits) - 2 // -2 for network and broadcast

	// Server is always at host offset 1 (first usable IP)
	// Peers start at offset 2 (minHostOffset)
	if maxHost < minHostOffset {
		return nil, fmt.Errorf("subnet /%d too small: no room for peers after server", maskBits)
	}

	// Get network base address
	netBase := ipNet.IP.To4()
	if netBase == nil {
		return nil, fmt.Errorf("could not determine network base for %s", serverCIDR)
	}

	return &ipManager{
		netBase:    netBase,
		serverHost: 1, // Always offset 1 from network base
		maskBits:   maskBits,
		maxHost:    maxHost,
	}, nil
}

func (m *ipManager) findNextAvailable(peers []PeerConfig) (string, int, error) {
	used := m.buildUsedIPMap(peers)

	for offset := minHostOffset; offset <= m.maxHost; offset++ {
		if !used[offset] {
			return m.makeIP(offset), offset, nil
		}
	}
	return "", 0, fmt.Errorf("no available IPs in subnet %s/%d", m.netBase, m.maskBits)
}

func (m *ipManager) buildUsedIPMap(peers []PeerConfig) map[int]bool {
	used := map[int]bool{
		0:             true, // network address (offset 0)
		m.maxHost + 1: true, // broadcast address
		m.serverHost:  true, // server always at offset 1
	}

	for _, p := range peers {
		for _, ip := range p.AllowedIPs {
			if offset := m.extractHostOffset(ip); offset > 0 {
				used[offset] = true
			}
		}
	}

	return used
}

// makeIP creates a /32 CIDR from a host offset within the subnet.
// For subnet 10.0.0.16/28 with offset 2: produces 10.0.0.18/32
func (m *ipManager) makeIP(hostOffset int) string {
	if len(m.netBase) < 4 {
		slog.Error("ipManager.makeIP: invalid netBase")
		return ""
	}
	// Add host offset to the network base's last octet
	lastOctet := int(m.netBase[3]) + hostOffset
	return fmt.Sprintf("%d.%d.%d.%d/32", m.netBase[0], m.netBase[1], m.netBase[2], lastOctet)
}

// extractHostOffset calculates the host offset from the network base.
// For subnet 10.0.0.16/28 and IP 10.0.0.18: returns 2
func (m *ipManager) extractHostOffset(ipCIDR string) int {
	ipStr := ipCIDR
	if before, _, ok := strings.Cut(ipCIDR, "/"); ok {
		ipStr = before
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return -1
	}

	v4 := ip.To4()
	if v4 == nil {
		return -1 // Not IPv4
	}

	if len(m.netBase) < 4 {
		return -1
	}

	// Verify first 3 octets match the network base
	if v4[0] != m.netBase[0] || v4[1] != m.netBase[1] || v4[2] != m.netBase[2] {
		return -1 // Different subnet
	}

	// Calculate offset from network base
	offset := int(v4[3]) - int(m.netBase[3])
	if offset < 0 || offset > m.maxHost+1 {
		return -1 // Outside subnet range
	}

	return offset
}

func normalizeCIDRv4Host(cidr string) (string, int, error) {
	ip, ipNet, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return cidr, 0, err
	}

	v4 := ip.To4()
	if v4 == nil {
		return cidr, 0, nil // Not IPv4
	}

	// If IP is the network address, increment to first usable host
	if v4.Equal(ipNet.IP.To4()) {
		host := make(net.IP, len(v4))
		copy(host, v4)
		host[3]++
		ones, _ := ipNet.Mask.Size()
		return fmt.Sprintf("%s/%d", host.String(), ones), int(host[3]), nil
	}

	// Otherwise keep as-is
	ones, _ := ipNet.Mask.Size()
	return fmt.Sprintf("%s/%d", v4.String(), ones), int(v4[3]), nil
}

func normalizeAddresses(addrs []string) []string {
	result := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		normalized, _, err := normalizeCIDRv4Host(addr)
		if err != nil {
			result = append(result, addr) // Keep original if not IPv4 CIDR
		} else {
			result = append(result, normalized)
		}
	}
	return result
}
