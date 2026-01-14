package wireguard

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gopkg.in/ini.v1"

	"github.com/mordilloSan/go_logger/v2/logger"
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
	normalized, serverHost, err := normalizeCIDRv4Host(serverCIDR)
	if err != nil {
		logger.Errorf("newIPManager: normalizeCIDRv4Host failed for %s: %v", serverCIDR, err)
		return nil, err
	}

	netBase, err := ipv4NetBase(normalized)
	if err != nil {
		logger.Errorf("newIPManager: ipv4NetBase failed for %s: %v", normalized, err)
		return nil, err
	}

	return &ipManager{
		netBase:    netBase,
		serverHost: serverHost,
	}, nil
}

func (m *ipManager) findNextAvailable(peers []PeerConfig) (string, int, error) {
	used := m.buildUsedIPMap(peers)

	for i := minHostIP; i <= maxHostIP; i++ {
		if !used[i] {
			return m.makeIP(i), i, nil
		}
	}

	logger.Errorf("ipManager: no available IPs in subnet %v", m.netBase)
	return "", 0, fmt.Errorf("no available IPs in subnet")
}

func (m *ipManager) buildUsedIPMap(peers []PeerConfig) map[int]bool {
	used := map[int]bool{
		0:   true, // network
		255: true, // broadcast
	}

	if m.serverHost > 0 {
		used[m.serverHost] = true
	}

	for _, p := range peers {
		for _, ip := range p.AllowedIPs {
			if host := extractHostOctet(ip); host > 0 {
				used[host] = true
			}
		}
	}

	return used
}

func (m *ipManager) makeIP(host int) string {
	return fmt.Sprintf("%d.%d.%d.%d/32", m.netBase[0], m.netBase[1], m.netBase[2], host)
}

func extractHostOctet(ip string) int {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return -1
	}

	last := parts[3]
	if idx := strings.Index(last, "/"); idx != -1 {
		last = last[:idx]
	}

	host, err := strconv.Atoi(last)
	if err != nil {
		return -1
	}
	return host
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

func ipv4NetBase(cidr string) (net.IP, error) {
	_, ipNet, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return nil, err
	}
	return ipNet.IP.To4(), nil
}

// getDefaultGatewayIPv4 reads /proc/net/route and returns the default gateway.
func getDefaultGatewayIPv4() (string, error) {
	return getGatewayFromRouteFile("")
}

// getGatewayForInterfaceIPv4 returns the default gateway for a specific interface.
func getGatewayForInterfaceIPv4(iface string) (string, error) {
	return getGatewayFromRouteFile(iface)
}

func getGatewayFromRouteFile(matchIface string) (string, error) {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		logger.Errorf("getGatewayFromRouteFile: read /proc/net/route failed: %v", err)
		return "", fmt.Errorf("read /proc/net/route: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// fields: Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
	for i := 1; i < len(lines); i++ {
		fields := strings.Fields(lines[i])
		if len(fields) < 3 {
			continue
		}
		iface := fields[0]
		dest := fields[1]
		gwHex := fields[2]

		// Only default route (Destination == 00000000)
		if dest != "00000000" {
			continue
		}
		if matchIface != "" && iface != matchIface {
			continue
		}
		gwIP, err := hexLEToIPv4(gwHex)
		if err != nil || gwIP == "0.0.0.0" {
			continue
		}
		return gwIP, nil
	}
	logger.Errorf("getGatewayFromRouteFile: default gateway not found (iface=%q)", matchIface)
	return "", fmt.Errorf("default gateway not found")
}

func hexLEToIPv4(hexStr string) (string, error) {
	u, err := strconv.ParseUint(hexStr, 16, 32)
	if err != nil {
		return "", err
	}
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(u))
	return net.IPv4(b[0], b[1], b[2], b[3]).String(), nil
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

	for i := 0; i < count; i++ {
		// Find next available IP
		var peerIP string
		var host int
		for h := minHostIP; h <= maxHostIP; h++ {
			if !used[h] {
				peerIP = ipMgr.makeIP(h)
				host = h
				used[h] = true
				break
			}
		}

		if peerIP == "" {
			logger.Errorf("generatePeers: insufficient IPs for %d peers", count)
			return nil, fmt.Errorf("insufficient IPs for %d peers", count)
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
			Name:                fmt.Sprintf("Peer%d", host),
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

func cleanBackticks(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	cleaned := strings.ReplaceAll(string(data), "`", "")
	return os.WriteFile(path, []byte(cleaned), 0o600)
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
