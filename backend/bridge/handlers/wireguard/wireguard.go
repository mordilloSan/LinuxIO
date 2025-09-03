package wireguard

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"gopkg.in/ini.v1"

	"github.com/mordilloSan/LinuxIO/internal/ipc"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/utils"
)

// --- Constants ---
const (
	wgConfigDir      = "/etc/wireguard"
	configExt        = ".conf"
	defaultKeepalive = 25
	maxHostIP        = 254
	minHostIP        = 2
)

// --- Types ---
type InterfaceConfig struct {
	PrivateKey string           `json:"private_key"`
	Address    []string         `json:"address"`
	ListenPort int              `json:"listen_port"`
	DNS        []string         `json:"dns"`
	MTU        int              `json:"mtu"`
	Peers      []ipc.PeerConfig `json:"peers"`
}

type WireGuardInterfaceUI struct {
	Name        string `json:"name"`
	IsConnected string `json:"isConnected"`
	Address     string `json:"address"`
	Port        int    `json:"port"`
	PeerCount   int    `json:"peerCount"`
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

// --- Config Parsing ---
func ParseWireGuardConfig(path string) (InterfaceConfig, error) {
	var cfg InterfaceConfig

	iniFile, err := ini.LoadSources(ini.LoadOptions{
		AllowNonUniqueSections: true,
	}, path)
	if err != nil {
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

		pc := ipc.PeerConfig{
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
			logger.Warnf("Failed to add peer %s: %v", peer.PublicKey, err)
			continue
		}
	}

	// Save file
	if err := iniFile.SaveTo(path); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// Remove backticks if PostUp/PostDown were added
	if includePostUpDown {
		return cleanBackticks(path)
	}

	return nil
}

func addPeerSection(iniFile *ini.File, peer ipc.PeerConfig) error {
	psec, err := iniFile.NewSection("Peer")
	if err != nil {
		return err
	}

	setKey(psec, "PublicKey", peer.PublicKey)
	setKeyIfNotEmpty(psec, "PresharedKey", peer.PresharedKey)
	setKeyIfNotEmpty(psec, "AllowedIPs", strings.Join(peer.AllowedIPs, ","))
	setKeyIfNotEmpty(psec, "Endpoint", peer.Endpoint)
	setKeyIfPositive(psec, "PersistentKeepalive", peer.PersistentKeepalive)

	return nil
}

// --- Peer Management ---
func ExportPeerConfig(interfaceName string, peer ipc.PeerConfig, ifaceCfg InterfaceConfig, publicIP string, peerNumber int, dnsOverride string) (string, error) {

	// Ensure peer directory exists
	peerDir := peerDirPath(interfaceName)
	if err := os.MkdirAll(peerDir, 0700); err != nil {
		return "", fmt.Errorf("create peer dir: %w", err)
	}

	peerPath := filepath.Join(peerDir, fmt.Sprintf("Peer%d.conf", peerNumber))
	iniFile := ini.Empty()

	// Create Interface section for peer
	ifSec, err := iniFile.NewSection("Interface")
	if err != nil {
		return "", fmt.Errorf("create interface section: %w", err)
	}

	// Set peer interface configuration
	if len(peer.AllowedIPs) > 0 {
		setKey(ifSec, "Address", peer.AllowedIPs[0])
	}
	setKeyIfPositive(ifSec, "ListenPort", ifaceCfg.ListenPort)

	if peer.PrivateKey == "" {
		return "", fmt.Errorf("peer private key is empty")
	}
	setKey(ifSec, "PrivateKey", peer.PrivateKey)

	// DNS precedence: interface DNS > override > none
	dnsVal := ""
	if len(ifaceCfg.DNS) > 0 {
		dnsVal = strings.Join(ifaceCfg.DNS, ",")
	} else if dnsOverride != "" {
		dnsVal = dnsOverride
	}
	setKeyIfNotEmpty(ifSec, "DNS", dnsVal)

	// Create Peer section (connecting to server)
	peerSec, err := iniFile.NewSection("Peer")
	if err != nil {
		return "", fmt.Errorf("create peer section: %w", err)
	}

	// Get server public key
	serverKey, err := wgtypes.ParseKey(ifaceCfg.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("parse server key: %w", err)
	}

	setKey(peerSec, "PublicKey", serverKey.PublicKey().String())
	setKey(peerSec, "AllowedIPs", "0.0.0.0/1, 128.0.0.0/1, ::/0")

	// Set endpoint if we have public IP - using simple format
	if publicIP != "" && ifaceCfg.ListenPort > 0 {
		endpoint := fmt.Sprintf("%s:%d", publicIP, ifaceCfg.ListenPort)
		setKey(peerSec, "Endpoint", endpoint)
	}

	setKeyIfNotEmpty(peerSec, "PresharedKey", peer.PresharedKey)
	setKeyIfPositive(peerSec, "PersistentKeepalive", peer.PersistentKeepalive)

	// Save peer config
	if err := iniFile.SaveTo(peerPath); err != nil {
		return "", fmt.Errorf("save peer config: %w", err)
	}

	return peerPath, nil
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

func isPeerSection(name string) bool {
	return name == "Peer" || strings.HasPrefix(name, "Peer ")
}

// --- IP Address Management ---
type ipManager struct {
	netBase    net.IP
	serverHost int
}

func newIPManager(serverCIDR string) (*ipManager, error) {
	normalized, serverHost, err := normalizeCIDRv4Host(serverCIDR)
	if err != nil {
		return nil, err
	}

	netBase, err := ipv4NetBase(normalized)
	if err != nil {
		return nil, err
	}

	return &ipManager{
		netBase:    netBase,
		serverHost: serverHost,
	}, nil
}

func (m *ipManager) findNextAvailable(peers []ipc.PeerConfig) (string, int, error) {
	used := m.buildUsedIPMap(peers)

	for i := minHostIP; i <= maxHostIP; i++ {
		if !used[i] {
			return m.makeIP(i), i, nil
		}
	}

	return "", 0, fmt.Errorf("no available IPs in subnet")
}

func (m *ipManager) buildUsedIPMap(peers []ipc.PeerConfig) map[int]bool {
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

// --- Handler Implementations ---
func ListInterfaces([]string) (any, error) {
	logger.Debugf("Listing interfaces")

	pattern := filepath.Join(wgConfigDir, "*"+configExt)
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}

	interfaces := make([]WireGuardInterfaceUI, 0, len(files))

	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), configExt)
		cfg, err := ParseWireGuardConfig(f)
		if err != nil {
			logger.Warnf("Failed to parse %s: %v", name, err)
			continue
		}

		status := "Inactive"
		if isInterfaceUp(name) {
			status = "Active"
		}

		interfaces = append(interfaces, WireGuardInterfaceUI{
			Name:        name,
			IsConnected: status,
			Address:     strings.Join(cfg.Address, ", "),
			Port:        cfg.ListenPort,
			PeerCount:   len(cfg.Peers),
		})
	}

	logger.Infof("Found %d interfaces", len(interfaces))
	return interfaces, nil
}

func AddInterface(args []string) (any, error) {
	if len(args) < 4 {
		return nil, fmt.Errorf("usage: add_interface <name> <addresses> <listenPort> <egressNic> [dns] [mtu] [peers_json] [numPeers]")
	}

	name := args[0]
	addresses := normalizeAddresses(parseCSV(args[1]))
	listenPort, _ := strconv.Atoi(args[2])
	egressNic := args[3]

	// Optional parameters
	dns := parseOptionalCSV(args, 4)
	mtu := parseOptionalInt(args, 5, 0)
	peers := parseOptionalPeers(args, 6)
	numPeers := parseOptionalInt(args, 7, 0)

	// Auto-generate peers if requested
	if len(peers) == 0 && numPeers > 0 {
		var err error
		peers, err = generatePeers(addresses[0], numPeers)
		if err != nil {
			return nil, fmt.Errorf("generate peers: %w", err)
		}
	}

	// Generate server keys
	privKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("generate private key: %w", err)
	}

	cfg := InterfaceConfig{
		PrivateKey: privKey.String(),
		Address:    addresses,
		ListenPort: listenPort,
		DNS:        dns,
		MTU:        mtu,
		Peers:      peers,
	}

	// Get public IP once
	publicIP, _ := utils.GetPublicIP()

	// Prefer the gateway of the selected egress nic
	gatewayDNS, _ := getGatewayForInterfaceIPv4(egressNic)

	// Export peer configs
	for _, peer := range peers {
		peerNumber := extractHostOctet(peer.AllowedIPs[0])
		if _, err := ExportPeerConfig(name, peer, cfg, publicIP, peerNumber, gatewayDNS); err != nil {
			logger.Warnf("Failed to export config for peer %s: %v", peer.PublicKey, err)
		}
	}

	// Write main config
	if err := WriteWireGuardConfigWithPostUpDown(configPath(name), cfg, egressNic); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}

	// Bring up interface
	if _, err := UpInterface([]string{name}); err != nil {
		return nil, err
	}

	logger.Infof("Interface %s created and brought up", name)

	return map[string]any{
		"status":      "created",
		"private_key": privKey.String(),
		"public_key":  privKey.PublicKey().String(),
		"peers":       peers,
	}, nil
}

func RemoveInterface(args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("usage: remove_interface <name>")
	}

	name := args[0]
	logger.Infof("Removing interface: %s", name)

	// Try to bring down interface (ignore errors)
	DownInterface([]string{name})

	// Remove config file
	if err := os.Remove(configPath(name)); err != nil {
		return nil, fmt.Errorf("remove config: %w", err)
	}

	// Remove peer directory
	peerDir := peerDirPath(name)
	if err := os.RemoveAll(peerDir); err != nil && !os.IsNotExist(err) {
		logger.Warnf("Could not remove peer dir %s: %v", peerDir, err)
	}

	logger.Infof("Interface %s removed", name)
	return "removed", nil
}

func AddPeer(args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("usage: add_peer <interface>")
	}

	interfaceName := args[0]

	// Read current config
	cfg, err := ParseWireGuardConfig(configPath(interfaceName))
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	if len(cfg.Address) == 0 {
		return nil, fmt.Errorf("interface has no address")
	}

	// Initialize IP manager
	ipMgr, err := newIPManager(cfg.Address[0])
	if err != nil {
		return nil, fmt.Errorf("init IP manager: %w", err)
	}

	// Find next available IP
	nextIP, peerNumber, err := ipMgr.findNextAvailable(cfg.Peers)
	if err != nil {
		return nil, err
	}

	// Generate keys
	priv, _ := wgtypes.GeneratePrivateKey()

	peer := ipc.PeerConfig{
		PublicKey:           priv.PublicKey().String(),
		PrivateKey:          priv.String(),
		AllowedIPs:          []string{nextIP},
		PersistentKeepalive: defaultKeepalive,
		Name:                fmt.Sprintf("Peer%d", peerNumber),
	}

	// Update config
	cfg.Peers = append(cfg.Peers, peer)
	if err := WriteWireGuardConfig(configPath(interfaceName), cfg); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}

	// Export peer config
	publicIP, _ := utils.GetPublicIP()
	gatewayDNS, _ := getDefaultGatewayIPv4()
	if _, err := ExportPeerConfig(interfaceName, peer, cfg, publicIP, peerNumber, gatewayDNS); err != nil {
		return nil, fmt.Errorf("export peer config: %w", err)
	}

	return map[string]any{
		"peer_name":  peer.Name,
		"public_key": peer.PublicKey,
		"allowed_ip": nextIP,
	}, nil
}

func RemovePeerByName(args []string) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("usage: remove_peer <interface> <peer>")
	}

	interfaceName := args[0]
	peerName := args[1]

	// Read peer config to get AllowedIP
	peerPath := peerConfigPath(interfaceName, peerName)

	// Extract Address from peer config
	iniFile, err := ini.Load(peerPath)
	if err != nil {
		return nil, fmt.Errorf("parse peer config: %w", err)
	}

	allowedIP := iniFile.Section("Interface").Key("Address").String()
	if allowedIP == "" {
		return nil, fmt.Errorf("peer config missing Address")
	}

	// Read main config
	cfg, err := ParseWireGuardConfig(configPath(interfaceName))
	if err != nil {
		return nil, fmt.Errorf("read main config: %w", err)
	}

	// Remove peer from config
	found := false
	newPeers := make([]ipc.PeerConfig, 0, len(cfg.Peers))
	for _, p := range cfg.Peers {
		// Check if this peer matches the AllowedIP
		match := false
		for _, ip := range p.AllowedIPs {
			if ip == allowedIP {
				match = true
				found = true
				break
			}
		}
		if !match {
			newPeers = append(newPeers, p)
		}
	}

	if !found {
		return nil, fmt.Errorf("peer not found with IP %s", allowedIP)
	}

	cfg.Peers = newPeers

	// Write updated config
	if err := WriteWireGuardConfig(configPath(interfaceName), cfg); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}

	// Remove peer config file
	if err := os.Remove(peerPath); err != nil && !os.IsNotExist(err) {
		logger.Warnf("Could not remove peer config: %v", err)
	}

	return "removed", nil
}

func ListPeers(args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("usage: list_exported_peers <interface>")
	}

	interfaceName := args[0]
	pattern := filepath.Join(peerDirPath(interfaceName), "*"+configExt)
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("list peer configs: %w", err)
	}

	peers := make([]ipc.PeerConfig, 0, len(files))

	for _, file := range files {
		iniFile, err := ini.Load(file)
		if err != nil {
			logger.Warnf("Failed to parse %s: %v", file, err)
			continue
		}

		ifSec := iniFile.Section("Interface")
		peerSec := iniFile.Section("Peer")

		pc := ipc.PeerConfig{
			PrivateKey:          ifSec.Key("PrivateKey").String(),
			AllowedIPs:          parseCSV(ifSec.Key("Address").String()),
			PublicKey:           peerSec.Key("PublicKey").String(),
			PresharedKey:        peerSec.Key("PresharedKey").String(),
			Endpoint:            peerSec.Key("Endpoint").String(),
			Name:                ifSec.Key("Name").String(),
			PersistentKeepalive: peerSec.Key("PersistentKeepalive").MustInt(0),
		}

		if pc.Name == "" {
			pc.Name = strings.TrimSuffix(filepath.Base(file), configExt)
		}

		peers = append(peers, pc)
	}

	return peers, nil
}

func UpInterface(args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("usage: up_interface <name>")
	}

	name := args[0]
	cmd := exec.Command("wg-quick", "up", name)
	out, err := cmd.CombinedOutput()

	if err != nil {
		logger.Errorf("Failed to bring up %s: %v (%s)", name, err, string(out))
		return nil, fmt.Errorf("bring up interface: %w", err)
	}

	logger.Infof("Interface %s brought up", name)
	return map[string]any{
		"status": "on",
		"output": string(out),
	}, nil
}

func DownInterface(args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("usage: down_interface <name>")
	}

	name := args[0]
	cmd := exec.Command("wg-quick", "down", name)
	out, err := cmd.CombinedOutput()

	if err != nil {
		logger.Errorf("Failed to bring down %s: %v (%s)", name, err, string(out))
		return nil, fmt.Errorf("bring down interface: %w", err)
	}

	logger.Infof("Interface %s brought down", name)
	return map[string]any{
		"status": "off",
		"output": string(out),
	}, nil
}

func PeerQRCode(args []string) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("usage: peer_qrcode <interface> <peername>")
	}

	peerPath := peerConfigPath(args[0], args[1])
	rawConfig, err := os.ReadFile(peerPath)
	if err != nil {
		return nil, fmt.Errorf("read peer config: %w", err)
	}

	// Generate QR code
	png, err := qrcode.Encode(string(rawConfig), qrcode.Medium, 256)
	if err != nil {
		return nil, fmt.Errorf("generate QR code: %w", err)
	}

	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	return map[string]string{"qrcode": dataURI}, nil
}

func PeerConfigDownload(args []string) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("usage: peer_config_download <interface> <peername>")
	}

	peerPath := peerConfigPath(args[0], args[1])
	data, err := os.ReadFile(peerPath)
	if err != nil {
		return nil, fmt.Errorf("read peer config: %w", err)
	}

	return map[string]string{"config": string(data)}, nil
}

func GetKeys(args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("usage: get_keys <interface>")
	}

	cfg, err := ParseWireGuardConfig(configPath(args[0]))
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	key, err := wgtypes.ParseKey(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("parse key: %w", err)
	}

	return map[string]string{
		"private_key": cfg.PrivateKey,
		"public_key":  key.PublicKey().String(),
	}, nil
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

func parseOptionalPeers(args []string, index int) []ipc.PeerConfig {
	if index < len(args) && args[index] != "" && args[index] != "null" && args[index] != "[]" {
		var peers []ipc.PeerConfig
		if err := json.Unmarshal([]byte(args[index]), &peers); err == nil {
			return peers
		}
	}
	return nil
}

func generatePeers(serverAddr string, count int) ([]ipc.PeerConfig, error) {
	ipMgr, err := newIPManager(serverAddr)
	if err != nil {
		return nil, err
	}

	peers := make([]ipc.PeerConfig, 0, count)
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
			return nil, fmt.Errorf("insufficient IPs for %d peers", count)
		}

		// Generate keys
		privKey, err := wgtypes.GeneratePrivateKey()
		if err != nil {
			return nil, fmt.Errorf("generate key for peer %d: %w", i+1, err)
		}

		peers = append(peers, ipc.PeerConfig{
			PublicKey:           privKey.PublicKey().String(),
			PrivateKey:          privKey.String(),
			AllowedIPs:          []string{peerIP},
			PersistentKeepalive: defaultKeepalive,
			Name:                fmt.Sprintf("Peer%d", host),
		})
	}

	return peers, nil
}

// --- INI Helper Functions ---
func setKey(section *ini.Section, key, value string) {
	if _, err := section.NewKey(key, value); err != nil {
		logger.Warnf("Failed to set %s: %v", key, err)
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
	return os.WriteFile(path, []byte(cleaned), 0600)
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
