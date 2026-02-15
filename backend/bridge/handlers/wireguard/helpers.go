package wireguard

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gopkg.in/ini.v1"

	"github.com/mordilloSan/go-logger/logger"
)

// validInterfaceName matches valid WireGuard interface names (alphanumeric, underscore, hyphen)
var validInterfaceName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// validateInterfaceName checks if the interface name is safe to use in file paths
func validateInterfaceName(name string) error {
	if name == "" {
		return fmt.Errorf("interface name cannot be empty")
	}
	if len(name) > 15 { // Linux interface name limit
		return fmt.Errorf("interface name too long (max 15 characters)")
	}
	if !validInterfaceName.MatchString(name) {
		return fmt.Errorf("interface name contains invalid characters (allowed: a-z, A-Z, 0-9, _, -)")
	}
	return nil
}

// --- Path Helpers ---
func configPath(name string) string {
	return filepath.Join(wgConfigDir, name+configExt)
}

func peerDirPath(iface string) string {
	return filepath.Join(wgConfigDir, iface)
}

func peerConfigPath(iface, peerName string) string {
	return filepath.Join(peerDirPath(iface), peerName+configExt)
}

// --- Network Helpers ---
func isInterfaceUp(name string) bool {
	return exec.Command("wg", "show", name).Run() == nil
}

func isInterfaceEnabled(name string) bool {
	serviceName := fmt.Sprintf("wg-quick@%s.service", name)
	cmd := exec.Command("systemctl", "is-enabled", serviceName)
	return cmd.Run() == nil
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

func newIPManager(serverCIDR string) (*ipManager, error) {
	ip, ipNet, err := net.ParseCIDR(strings.TrimSpace(serverCIDR))
	if err != nil {
		logger.Errorf("newIPManager: parse CIDR failed for %s: %v", serverCIDR, err)
		return nil, fmt.Errorf("invalid CIDR %q: %w", serverCIDR, err)
	}

	// Ensure IPv4 only
	v4 := ip.To4()
	if v4 == nil {
		logger.Errorf("newIPManager: %s is not an IPv4 address", serverCIDR)
		return nil, fmt.Errorf("IPv6 addresses are not supported: %s", serverCIDR)
	}

	maskBits, totalBits := ipNet.Mask.Size()
	if totalBits != 32 {
		logger.Errorf("newIPManager: unexpected mask size for %s", serverCIDR)
		return nil, fmt.Errorf("invalid IPv4 mask for %s", serverCIDR)
	}

	// Only support subnets within a /24 boundary (mask >= 24)
	// This simplifies IP math to last-octet operations
	if maskBits < 24 {
		logger.Errorf("newIPManager: subnet %s too large, max supported is /24", serverCIDR)
		return nil, fmt.Errorf("subnet too large: max supported is /24, got /%d", maskBits)
	}

	// Calculate host range based on subnet mask
	// For /24: hostBits=8, maxHost=254 (2^8-2)
	// For /28: hostBits=4, maxHost=14 (2^4-2)
	hostBits := 32 - maskBits
	if hostBits < 2 {
		logger.Errorf("newIPManager: subnet %s too small for peer allocation", serverCIDR)
		return nil, fmt.Errorf("subnet too small: need at least /30, got /%d", maskBits)
	}
	maxHost := (1 << hostBits) - 2 // -2 for network and broadcast

	// Server is always at host offset 1 (first usable IP)
	// Peers start at offset 2 (minHostOffset)
	if maxHost < minHostOffset {
		logger.Errorf("newIPManager: subnet %s has no room for peers (server uses only usable IP)", serverCIDR)
		return nil, fmt.Errorf("subnet /%d too small: no room for peers after server", maskBits)
	}

	// Get network base address
	netBase := ipNet.IP.To4()
	if netBase == nil {
		logger.Errorf("newIPManager: could not get network base for %s", serverCIDR)
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

	logger.Errorf("ipManager: no available IPs in subnet %v (max host offset: %d)", m.netBase, m.maxHost)
	return "", 0, fmt.Errorf("no available IPs in subnet")
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
		logger.Errorf("ipManager.makeIP: invalid netBase")
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

// --- IPv4 Helper Functions ---
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

// getDefaultGatewayIPv4 returns the default IPv4 gateway.
func getDefaultGatewayIPv4() (string, error) {
	return getGatewayFromNetlink(0)
}

// getGatewayForInterfaceIPv4 returns the default IPv4 gateway for a specific interface.
func getGatewayForInterfaceIPv4(iface string) (string, error) {
	link, err := netlink.LinkByName(iface)
	if err != nil {
		logger.Errorf("getGatewayForInterfaceIPv4: lookup %s failed: %v", iface, err)
		return "", fmt.Errorf("lookup interface %s: %w", iface, err)
	}
	return getGatewayFromNetlink(link.Attrs().Index)
}

func getGatewayFromNetlink(linkIndex int) (string, error) {
	filter := &netlink.Route{
		Table: syscall.RT_TABLE_MAIN,
	}
	mask := netlink.RT_FILTER_TABLE
	if linkIndex > 0 {
		filter.LinkIndex = linkIndex
		mask |= netlink.RT_FILTER_OIF
	}

	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, filter, mask)
	if err != nil {
		logger.Errorf("getGatewayFromNetlink: list routes failed: %v", err)
		return "", fmt.Errorf("list routes: %w", err)
	}

	for _, r := range routes {
		if r.Dst != nil {
			continue
		}
		if r.Gw == nil || r.Gw.IsUnspecified() {
			continue
		}
		return r.Gw.String(), nil
	}

	logger.Debugf("getGatewayFromNetlink: no default gateway found (linkIndex=%d)", linkIndex)
	return "", fmt.Errorf("default gateway not found")
}

// --- Helper functions ---
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

func parseOptionalCSV(args []string, index int) []string {
	if index < len(args) && args[index] != "" && args[index] != "null" {
		return parseCSV(args[index])
	}
	return nil
}

func parseOptionalInt(args []string, index int, defaultVal int) int {
	if index < len(args) && args[index] != "" && args[index] != "null" {
		val, _ := strconv.Atoi(args[index])
		return val
	}
	return defaultVal
}

func parseOptionalPeers(args []string, index int) []PeerConfig {
	if index < len(args) && args[index] != "" && args[index] != "null" && args[index] != "[]" {
		var peers []PeerConfig
		if err := json.Unmarshal([]byte(args[index]), &peers); err == nil {
			return peers
		}
	}
	return nil
}

func generatePeers(serverAddr string, count int) ([]PeerConfig, error) {
	ipMgr, err := newIPManager(serverAddr)
	if err != nil {
		logger.Errorf("generatePeers: newIPManager failed: %v", err)
		return nil, err
	}

	peers := make([]PeerConfig, 0, count)
	used := ipMgr.buildUsedIPMap(nil)

	for i := range count {
		// Find next available IP using ipManager's computed maxHost
		var peerIP string
		var hostOffset int
		for offset := minHostOffset; offset <= ipMgr.maxHost; offset++ {
			if !used[offset] {
				peerIP = ipMgr.makeIP(offset)
				hostOffset = offset
				used[offset] = true
				break
			}
		}

		if peerIP == "" {
			logger.Errorf("generatePeers: insufficient IPs for %d peers (subnet /%d)", count, ipMgr.maskBits)
			return nil, fmt.Errorf("insufficient IPs for %d peers in /%d subnet", count, ipMgr.maskBits)
		}

		// Generate keys
		privKey, err := wgtypes.GeneratePrivateKey()
		if err != nil {
			logger.Errorf("generatePeers: generate key for peer %d failed: %v", i+1, err)
			return nil, fmt.Errorf("generate key for peer %d: %w", i+1, err)
		}

		peers = append(peers, PeerConfig{
			PublicKey:           privKey.PublicKey().String(),
			PrivateKey:          privKey.String(),
			AllowedIPs:          []string{peerIP},
			PersistentKeepalive: defaultKeepalive,
			Name:                fmt.Sprintf("Peer%d", hostOffset),
		})
	}

	logger.Infof("generatePeers: generated %d peers for server %s", count, serverAddr)
	return peers, nil
}

// --- INI Helper Functions ---
func setKey(section *ini.Section, key, value string) {
	if _, err := section.NewKey(key, value); err != nil {
		logger.Warnf("setKey: failed to set %s: %v", key, err)
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

// for wireguard stats

// --- rate cache for bytes/sec ---
type rateSample struct {
	rx int64
	tx int64
	ts time.Time
}

var (
	rateMu    sync.Mutex
	rateCache = map[string]rateSample{} // key: iface + "|" + publicKey
)

func computeRates(iface, pub string, rx, tx int64, now time.Time) (rxBps, txBps float64) {
	key := iface + "|" + pub

	rateMu.Lock()
	defer rateMu.Unlock()

	prev, ok := rateCache[key]
	// store current sample immediately
	rateCache[key] = rateSample{rx: rx, tx: tx, ts: now}

	// first observation -> no rate yet
	if !ok {
		return 0, 0
	}

	dt := now.Sub(prev.ts).Seconds()
	if dt <= 0.4 { // avoid noisy very-short intervals
		return 0, 0
	}

	// counters can reset when interface bounces; guard negatives
	if rx < prev.rx || tx < prev.tx {
		return 0, 0
	}

	return float64(rx-prev.rx) / dt, float64(tx-prev.tx) / dt
}
