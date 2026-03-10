package dbus

import (
	"fmt"
	stdnet "net"
	"os"
	"strconv"
	"strings"
	"time"

	godbus "github.com/godbus/dbus/v5"
	"github.com/mordilloSan/go-logger/logger"
	"github.com/shirou/gopsutil/v4/net"
)

// NMInterfaceInfo contains comprehensive network interface information
type NMInterfaceInfo struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	MAC          string   `json:"mac"`
	MTU          uint32   `json:"mtu"`
	Speed        string   `json:"speed"`
	Duplex       string   `json:"duplex"`
	State        uint32   `json:"state"`
	IP4Addresses []string `json:"ipv4"`
	IP6Addresses []string `json:"ipv6"`
	RxSpeed      float64  `json:"rx_speed"`
	TxSpeed      float64  `json:"tx_speed"`
	DNS          []string `json:"dns"`
	Gateway      string   `json:"gateway"`
	IPv4Method   string   `json:"ipv4_method"` // "auto", "manual", "disabled", etc.
}

var (
	lastNetStats  = make(map[string]net.IOCountersStat)
	lastTimestamp int64
)

type nmConnParts struct {
	DevicePath   godbus.ObjectPath
	ActivePath   godbus.ObjectPath
	SettingsPath godbus.ObjectPath
	UUID         string
}

// --- Helper Functions ---

func getActiveConnForIface(conn *godbus.Conn, iface string) (*nmConnParts, error) {
	nm := conn.Object("org.freedesktop.NetworkManager", "/org/freedesktop/NetworkManager")

	var devs []godbus.ObjectPath
	if err := nm.Call("org.freedesktop.NetworkManager.GetDevices", 0).Store(&devs); err != nil {
		return nil, fmt.Errorf("GetDevices failed: %w", err)
	}

	for _, dp := range devs {
		dev := conn.Object("org.freedesktop.NetworkManager", dp)
		var devIface string
		if err := dev.Call("org.freedesktop.DBus.Properties.Get", 0,
			"org.freedesktop.NetworkManager.Device", "Interface").Store(&devIface); err != nil {
			continue
		}
		if devIface != iface {
			continue
		}

		var active godbus.ObjectPath
		if err := dev.Call("org.freedesktop.DBus.Properties.Get", 0,
			"org.freedesktop.NetworkManager.Device", "ActiveConnection").Store(&active); err != nil {
			return nil, fmt.Errorf("get ActiveConnection: %w", err)
		}
		if active == "" || active == "/" {
			return nil, fmt.Errorf("no active connection for %s", iface)
		}

		ac := conn.Object("org.freedesktop.NetworkManager", active)
		var connPath godbus.ObjectPath
		if err := ac.Call("org.freedesktop.DBus.Properties.Get", 0,
			"org.freedesktop.NetworkManager.Connection.Active", "Connection").Store(&connPath); err != nil {
			return nil, fmt.Errorf("get Connection path: %w", err)
		}

		settingsConn := conn.Object("org.freedesktop.NetworkManager", connPath)
		var settings map[string]map[string]godbus.Variant
		if err := settingsConn.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0).Store(&settings); err != nil {
			return nil, fmt.Errorf("GetSettings: %w", err)
		}
		uuid, _ := settings["connection"]["uuid"].Value().(string)
		if uuid == "" {
			return nil, fmt.Errorf("connection uuid not found")
		}

		return &nmConnParts{
			DevicePath:   dp,
			ActivePath:   active,
			SettingsPath: connPath,
			UUID:         uuid,
		}, nil
	}
	return nil, fmt.Errorf("interface %s not found", iface)
}

func mapDeviceType(devType uint32) string {
	switch devType {
	case 1:
		return "ethernet"
	case 2:
		return "wifi"
	case 5:
		return "bt"
	case 6:
		return "olpc-mesh"
	case 7:
		return "wimax"
	case 8:
		return "modem"
	case 9:
		return "infiniband"
	case 10:
		return "bond"
	case 11:
		return "vlan"
	case 14:
		return "bridge"
	case 17:
		return "tun"
	case 27:
		return "ovs-bridge"
	default:
		return "unknown"
	}
}

func getIPv4Method(conn *godbus.Conn, iface string) string {
	parts, err := getActiveConnForIface(conn, iface)
	if err != nil {
		return "unknown"
	}

	settingsConn := conn.Object("org.freedesktop.NetworkManager", parts.SettingsPath)
	var settings map[string]map[string]godbus.Variant
	if err := settingsConn.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0).Store(&settings); err != nil {
		return "unknown"
	}

	if ip4, ok := settings["ipv4"]; ok {
		if methodVariant, ok := ip4["method"]; ok {
			if method, ok := methodVariant.Value().(string); ok {
				return method
			}
		}
	}

	return "unknown"
}

func reloadConnection(conn *godbus.Conn, connPath godbus.ObjectPath) error {
	nm := conn.Object("org.freedesktop.NetworkManager", "/org/freedesktop/NetworkManager")

	var activeConns []godbus.ObjectPath
	if err := nm.Call("org.freedesktop.DBus.Properties.Get", 0,
		"org.freedesktop.NetworkManager", "ActiveConnections").Store(&activeConns); err != nil {
		return fmt.Errorf("failed to get active connections: %w", err)
	}

	var connToDeactivate godbus.ObjectPath
	for _, ac := range activeConns {
		acObj := conn.Object("org.freedesktop.NetworkManager", ac)
		var c godbus.ObjectPath
		if err := acObj.Call("org.freedesktop.DBus.Properties.Get", 0,
			"org.freedesktop.NetworkManager.Connection.Active", "Connection").Store(&c); err == nil {
			if c == connPath {
				connToDeactivate = ac
				break
			}
		}
	}

	if connToDeactivate != "" {
		if err := nm.Call("org.freedesktop.NetworkManager.DeactivateConnection", 0, connToDeactivate).Err; err != nil {
			return fmt.Errorf("failed to deactivate connection: %w", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	var devicePath godbus.ObjectPath = "/"
	var specificObject godbus.ObjectPath = "/"
	if err := nm.Call("org.freedesktop.NetworkManager.ActivateConnection", 0,
		connPath, devicePath, specificObject).Err; err != nil {
		return fmt.Errorf("failed to reactivate connection: %w", err)
	}

	return nil
}

// --- Network Information ---

func GetNetworkInfo() ([]NMInterfaceInfo, error) {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	snapshotMap, now, interval := currentNetworkSnapshot()
	var results []NMInterfaceInfo

	err := RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.ConnectSystemBus()
		if err != nil {
			return fmt.Errorf("failed to connect to system bus: %w", err)
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil {
				logger.Warnf("failed to close D-Bus connection: %v", cerr)
			}
		}()
		devicePaths, err := listNetworkManagerDevices(conn)
		if err != nil {
			return err
		}
		results = collectNMInterfaceInfo(conn, devicePaths, snapshotMap, interval)
		return nil
	})

	lastTimestamp = now

	return results, err
}

func currentNetworkSnapshot() (map[string]net.IOCountersStat, int64, int64) {
	snapshots, _ := net.IOCounters(true)
	snapshotMap := make(map[string]net.IOCountersStat, len(snapshots))
	for _, snapshot := range snapshots {
		snapshotMap[snapshot.Name] = snapshot
	}
	now := time.Now().Unix()
	return snapshotMap, now, max(now-lastTimestamp, 1)
}

func listNetworkManagerDevices(conn *godbus.Conn) ([]godbus.ObjectPath, error) {
	nm := conn.Object("org.freedesktop.NetworkManager", "/org/freedesktop/NetworkManager")
	var devicePaths []godbus.ObjectPath
	if err := nm.Call("org.freedesktop.NetworkManager.GetDevices", 0).Store(&devicePaths); err != nil {
		return nil, fmt.Errorf("GetDevices failed: %w", err)
	}
	return devicePaths, nil
}

func collectNMInterfaceInfo(
	conn *godbus.Conn,
	devicePaths []godbus.ObjectPath,
	snapshotMap map[string]net.IOCountersStat,
	interval int64,
) []NMInterfaceInfo {
	results := make([]NMInterfaceInfo, 0, len(devicePaths))
	for _, devPath := range devicePaths {
		info, ok := loadNMInterfaceInfo(conn, devPath, snapshotMap, interval)
		if ok {
			results = append(results, info)
		}
	}
	return results
}

func loadNMInterfaceInfo(
	conn *godbus.Conn,
	devPath godbus.ObjectPath,
	snapshotMap map[string]net.IOCountersStat,
	interval int64,
) (NMInterfaceInfo, bool) {
	props, err := networkManagerDeviceProperties(conn, devPath)
	if err != nil {
		return NMInterfaceInfo{}, false
	}

	name, mac, devType, mtu, state := basicNMDeviceInfo(props)
	ip4s, dns, gateway := loadIPv4Config(conn, props)
	ip6s, dns6, gateway6 := loadIPv6Config(conn, props)
	dns = append(dns, dns6...)
	if gateway == "" {
		gateway = gateway6
	}
	rxSpeed, txSpeed := networkInterfaceSpeed(name, snapshotMap, interval)

	return NMInterfaceInfo{
		Name:         name,
		Type:         mapDeviceType(devType),
		MAC:          mac,
		MTU:          mtu,
		Speed:        networkInterfaceLinkSpeed(name),
		Duplex:       networkInterfaceDuplex(name),
		State:        state,
		IP4Addresses: ip4s,
		IP6Addresses: ip6s,
		RxSpeed:      rxSpeed,
		TxSpeed:      txSpeed,
		DNS:          dns,
		Gateway:      gateway,
		IPv4Method:   getIPv4Method(conn, name),
	}, true
}

func networkManagerDeviceProperties(conn *godbus.Conn, devPath godbus.ObjectPath) (map[string]godbus.Variant, error) {
	dev := conn.Object("org.freedesktop.NetworkManager", devPath)
	props := make(map[string]godbus.Variant)
	if err := dev.Call("org.freedesktop.DBus.Properties.GetAll", 0, "org.freedesktop.NetworkManager.Device").Store(&props); err != nil {
		return nil, err
	}
	return props, nil
}

func basicNMDeviceInfo(props map[string]godbus.Variant) (string, string, uint32, uint32, uint32) {
	name, _ := props["Interface"].Value().(string)
	mac, _ := props["HwAddress"].Value().(string)
	return name, mac, variantUint32(props, "DeviceType"), variantUint32(props, "Mtu"), variantUint32(props, "State")
}

func variantUint32(props map[string]godbus.Variant, key string) uint32 {
	variant, ok := props[key]
	if !ok || variant.Value() == nil {
		return 0
	}
	value, _ := variant.Value().(uint32)
	return value
}

func networkInterfaceLinkSpeed(name string) string {
	if name == "" {
		return "unknown"
	}
	if data, err := os.ReadFile(fmt.Sprintf("/sys/class/net/%s/speed", name)); err == nil {
		return strings.TrimSpace(string(data)) + " Mbps"
	}
	return "unknown"
}

func networkInterfaceDuplex(name string) string {
	if name == "" {
		return "unknown"
	}
	if data, err := os.ReadFile(fmt.Sprintf("/sys/class/net/%s/duplex", name)); err == nil {
		return strings.TrimSpace(string(data))
	}
	return "unknown"
}

func loadIPv4Config(conn *godbus.Conn, props map[string]godbus.Variant) ([]string, []string, string) {
	ip4Path, ok := props["Ip4Config"].Value().(godbus.ObjectPath)
	if !ok || ip4Path == "/" {
		return nil, nil, ""
	}
	ip4Obj := conn.Object("org.freedesktop.NetworkManager", ip4Path)
	var ip4Props map[string]godbus.Variant
	if err := ip4Obj.Call("org.freedesktop.DBus.Properties.GetAll", 0, "org.freedesktop.NetworkManager.IP4Config").Store(&ip4Props); err != nil {
		return nil, nil, ""
	}
	return parseIPv4Addresses(ip4Props), parseIPv4Nameservers(ip4Props), parseGateway(ip4Props)
}

func parseIPv4Addresses(ip4Props map[string]godbus.Variant) []string {
	addresses, ok := ip4Props["Addresses"].Value().([][]uint32)
	if !ok {
		return nil
	}
	ip4s := make([]string, 0, len(addresses))
	for _, addr := range addresses {
		ip4s = append(ip4s, fmt.Sprintf(
			"%d.%d.%d.%d/%d",
			byte(addr[0]), byte(addr[0]>>8), byte(addr[0]>>16), byte(addr[0]>>24), addr[1],
		))
	}
	return ip4s
}

func parseIPv4Nameservers(ip4Props map[string]godbus.Variant) []string {
	dnsAddrs, ok := ip4Props["Nameservers"].Value().([]uint32)
	if !ok {
		return nil
	}
	dns := make([]string, 0, len(dnsAddrs))
	for _, ip := range dnsAddrs {
		dns = append(dns, fmt.Sprintf("%d.%d.%d.%d", byte(ip), byte(ip>>8), byte(ip>>16), byte(ip>>24)))
	}
	return dns
}

func parseGateway(props map[string]godbus.Variant) string {
	gateway, _ := props["Gateway"].Value().(string)
	return gateway
}

func loadIPv6Config(conn *godbus.Conn, props map[string]godbus.Variant) ([]string, []string, string) {
	ip6Path, ok := props["Ip6Config"].Value().(godbus.ObjectPath)
	if !ok || ip6Path == "/" {
		return nil, nil, ""
	}
	ip6Obj := conn.Object("org.freedesktop.NetworkManager", ip6Path)
	var ip6Props map[string]godbus.Variant
	if err := ip6Obj.Call("org.freedesktop.DBus.Properties.GetAll", 0, "org.freedesktop.NetworkManager.IP6Config").Store(&ip6Props); err != nil {
		return nil, nil, ""
	}
	return parseIPv6Addresses(ip6Props), parseIPv6Nameservers(ip6Props), parseGateway(ip6Props)
}

func parseIPv6Addresses(ip6Props map[string]godbus.Variant) []string {
	addresses, ok := ip6Props["Addresses"].Value().([][]any)
	if !ok {
		return nil
	}
	ip6s := make([]string, 0, len(addresses))
	for _, tuple := range addresses {
		addrBytes, _ := tuple[0].([]byte)
		prefix, _ := tuple[1].(uint32)
		if len(addrBytes) == 16 {
			ip6s = append(ip6s, formatIPv6Bytes(addrBytes)+fmt.Sprintf("/%d", prefix))
		}
	}
	return ip6s
}

func parseIPv6Nameservers(ip6Props map[string]godbus.Variant) []string {
	dns6, ok := ip6Props["Nameservers"].Value().([][]byte)
	if !ok {
		return nil
	}
	dns := make([]string, 0, len(dns6))
	for _, addr := range dns6 {
		if len(addr) == 16 {
			dns = append(dns, formatIPv6Bytes(addr))
		}
	}
	return dns
}

func formatIPv6Bytes(addr []byte) string {
	parts := make([]string, 8)
	for i := range 8 {
		parts[i] = fmt.Sprintf("%02x%02x", addr[2*i], addr[2*i+1])
	}
	return strings.Join(parts, ":")
}

func networkInterfaceSpeed(name string, snapshotMap map[string]net.IOCountersStat, interval int64) (float64, float64) {
	snapshot, ok := snapshotMap[name]
	if !ok {
		return 0, 0
	}
	var rxSpeed, txSpeed float64
	if prev, ok := lastNetStats[name]; ok {
		rxSpeed = float64(snapshot.BytesRecv-prev.BytesRecv) / float64(interval)
		txSpeed = float64(snapshot.BytesSent-prev.BytesSent) / float64(interval)
	}
	lastNetStats[name] = snapshot
	return rxSpeed, txSpeed
}

// --- IPv4 Configuration ---

// SetIPv4Manual configures static IPv4 with IP address, gateway, and DNS in one atomic operation
func SetIPv4Manual(iface, addressCIDR, gateway string, dnsServers []string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	if err := validateIPv4ManualArgs(iface, addressCIDR, gateway, dnsServers); err != nil {
		return err
	}
	dnsUint32, err := parseIPv4DNSServers(dnsServers)
	if err != nil {
		return err
	}

	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.ConnectSystemBus()
		if err != nil {
			return fmt.Errorf("connect system bus: %w", err)
		}
		defer conn.Close()

		parts, err := getActiveConnForIface(conn, iface)
		if err != nil {
			return err
		}

		ip, prefix, err := parseIPv4CIDR(addressCIDR)
		if err != nil {
			return err
		}

		settingsConn := conn.Object("org.freedesktop.NetworkManager", parts.SettingsPath)
		nm := conn.Object("org.freedesktop.NetworkManager", "/org/freedesktop/NetworkManager")
		dev := conn.Object("org.freedesktop.NetworkManager", parts.DevicePath)

		if err := disableIPv4ForManualConfig(settingsConn, dev, nm, parts.ActivePath); err != nil {
			return err
		}
		return applyManualIPv4Config(settingsConn, nm, parts, ip, prefix, gateway, dnsUint32)
	})
}

func validateIPv4ManualArgs(iface, addressCIDR, gateway string, dnsServers []string) error {
	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("interface is required")
	}
	if strings.TrimSpace(addressCIDR) == "" {
		return fmt.Errorf("IP address is required")
	}
	if strings.TrimSpace(gateway) == "" {
		return fmt.Errorf("gateway is required")
	}
	if len(dnsServers) == 0 {
		return fmt.Errorf("at least one DNS server is required")
	}
	return nil
}

func parseIPv4DNSServers(dnsServers []string) ([]uint32, error) {
	dnsUint32 := make([]uint32, 0, len(dnsServers))
	for _, dnsStr := range dnsServers {
		dnsVal, err := parseIPv4DNSValue(dnsStr)
		if err != nil {
			return nil, err
		}
		dnsUint32 = append(dnsUint32, dnsVal)
	}
	return dnsUint32, nil
}

func parseIPv4DNSValue(dnsStr string) (uint32, error) {
	dnsParts := strings.Split(strings.TrimSpace(dnsStr), ".")
	if len(dnsParts) != 4 {
		return 0, fmt.Errorf("invalid DNS server format: %s", dnsStr)
	}
	var dnsVal uint32
	for i, part := range dnsParts {
		num, err := strconv.ParseUint(part, 10, 8)
		if err != nil {
			return 0, fmt.Errorf("invalid DNS server: %s", dnsStr)
		}
		dnsVal |= uint32(num) << (uint(i) * 8)
	}
	return dnsVal, nil
}

func parseIPv4CIDR(addressCIDR string) (string, uint32, error) {
	parts := strings.SplitN(strings.TrimSpace(addressCIDR), "/", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid IPv4 CIDR: %q", addressCIDR)
	}
	prefixValue, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("invalid IPv4 prefix: %q", parts[1])
	}
	return strings.TrimSpace(parts[0]), uint32(prefixValue), nil
}

func disableIPv4ForManualConfig(
	settingsConn godbus.BusObject,
	dev godbus.BusObject,
	nm godbus.BusObject,
	activePath godbus.ObjectPath,
) error {
	settings, err := getConnectionSettings(settingsConn, "GetSettings")
	if err != nil {
		return err
	}
	sanitizeConnectionSettings(settings)

	ip4 := ensureConnectionSection(settings, "ipv4")
	ip4["method"] = godbus.MakeVariant("disabled")
	delete(ip4, "address-data")
	delete(ip4, "dns")
	delete(ip4, "gateway")
	delete(ip4, "dhcp-client-id")
	delete(ip4, "dhcp-timeout")
	settings["ipv4"] = ip4

	if err := updateConnectionSettings(settingsConn, settings, "disable IPv4"); err != nil {
		return err
	}
	if err := dev.Call("org.freedesktop.NetworkManager.Device.Disconnect", 0).Err; err != nil {
		return fmt.Errorf("device disconnect: %w", err)
	}
	if activePath != "" && activePath != "/" {
		if err := nm.Call("org.freedesktop.NetworkManager.DeactivateConnection", 0, activePath).Err; err != nil {
			return fmt.Errorf("deactivate connection: %w", err)
		}
	}
	time.Sleep(1500 * time.Millisecond)
	return nil
}

func applyManualIPv4Config(
	settingsConn godbus.BusObject,
	nm godbus.BusObject,
	parts *nmConnParts,
	ip string,
	prefix uint32,
	gateway string,
	dnsUint32 []uint32,
) error {
	settings, err := getConnectionSettings(settingsConn, "GetSettings (2nd)")
	if err != nil {
		return err
	}
	sanitizeConnectionSettings(settings)

	ip4 := ensureConnectionSection(settings, "ipv4")
	ip4["address-data"] = godbus.MakeVariant([]map[string]godbus.Variant{{
		"address": godbus.MakeVariant(ip),
		"prefix":  godbus.MakeVariant(prefix),
	}})
	ip4["dns"] = godbus.MakeVariant(dnsUint32)
	ip4["gateway"] = godbus.MakeVariant(gateway)
	ip4["method"] = godbus.MakeVariant("manual")
	ip4["may-fail"] = godbus.MakeVariant(false)
	ip4["ignore-auto-dns"] = godbus.MakeVariant(true)
	ip4["ignore-auto-routes"] = godbus.MakeVariant(true)
	ip4["never-default"] = godbus.MakeVariant(false)
	settings["ipv4"] = ip4

	if err := updateConnectionSettings(settingsConn, settings, "set manual config"); err != nil {
		return err
	}
	var specificObject godbus.ObjectPath = "/"
	if err := nm.Call(
		"org.freedesktop.NetworkManager.ActivateConnection", 0,
		parts.SettingsPath, parts.DevicePath, specificObject,
	).Err; err != nil {
		return fmt.Errorf("reactivate: %w", err)
	}
	return nil
}

func getConnectionSettings(settingsConn godbus.BusObject, action string) (map[string]map[string]godbus.Variant, error) {
	var settings map[string]map[string]godbus.Variant
	if err := settingsConn.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0).Store(&settings); err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	return settings, nil
}

func sanitizeConnectionSettings(settings map[string]map[string]godbus.Variant) {
	if ip4 := settings["ipv4"]; ip4 != nil {
		delete(ip4, "addresses")
		delete(ip4, "address-data")
		delete(ip4, "routes")
		delete(ip4, "route-data")
		settings["ipv4"] = ip4
	}
	if ip6 := settings["ipv6"]; ip6 != nil {
		delete(ip6, "addresses")
		delete(ip6, "routes")
		delete(ip6, "route-data")
		settings["ipv6"] = ip6
	}
}

func ensureConnectionSection(settings map[string]map[string]godbus.Variant, key string) map[string]godbus.Variant {
	section := settings[key]
	if section == nil {
		section = make(map[string]godbus.Variant)
	}
	return section
}

func updateConnectionSettings(
	settingsConn godbus.BusObject,
	settings map[string]map[string]godbus.Variant,
	action string,
) error {
	if err := settingsConn.Call("org.freedesktop.NetworkManager.Settings.Connection.Update", 0, settings).Err; err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

// SetIPv4DHCP switches interface to DHCP (auto) mode
func SetIPv4DHCP(iface string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.ConnectSystemBus()
		if err != nil {
			return fmt.Errorf("connect system bus: %w", err)
		}
		defer conn.Close()

		parts, err := getActiveConnForIface(conn, iface)
		if err != nil {
			return err
		}

		settingsConn := conn.Object("org.freedesktop.NetworkManager", parts.SettingsPath)

		// Fetch current to keep unrelated sections intact
		var settings map[string]map[string]godbus.Variant
		if err := settingsConn.Call(
			"org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0,
		).Store(&settings); err != nil {
			return fmt.Errorf("GetSettings: %w", err)
		}

		ip4 := settings["ipv4"]
		if ip4 == nil {
			ip4 = make(map[string]godbus.Variant)
		}

		// Switch to DHCP
		ip4["method"] = godbus.MakeVariant("auto")

		// CLEAR manual addresses in BOTH formats:
		// 1) legacy ipv4.addresses (type: aau = array of [addr,prefix,gateway] as uint32)
		ip4["addresses"] = godbus.MakeVariant([][]uint32{}) // <-- important
		// 2) modern ipv4.address-data (type: aa{sv})
		ip4["address-data"] = godbus.MakeVariant([]map[string]godbus.Variant{})

		// CLEAR routes if you might have set them
		ip4["routes"] = godbus.MakeVariant([][]uint32{})
		ip4["route-data"] = godbus.MakeVariant([]map[string]godbus.Variant{})

		// CLEAR manual DNS & search, and re-enable DHCP-provided values
		ip4["dns"] = godbus.MakeVariant([]uint32{})
		ip4["dns-search"] = godbus.MakeVariant([]string{})
		ip4["ignore-auto-dns"] = godbus.MakeVariant(false)
		ip4["ignore-auto-routes"] = godbus.MakeVariant(false)
		ip4["never-default"] = godbus.MakeVariant(false)

		// REMOVE scalars that must not exist in DHCP mode
		delete(ip4, "gateway")
		delete(ip4, "dns-priority")
		delete(ip4, "may-fail")

		settings["ipv4"] = ip4

		// Don't touch ipv6 types; only delete typed fields if present
		if ip6 := settings["ipv6"]; ip6 != nil {
			delete(ip6, "addresses")
			delete(ip6, "address-data")
			delete(ip6, "routes")
			delete(ip6, "route-data")
			settings["ipv6"] = ip6
		}

		if err := settingsConn.Call(
			"org.freedesktop.NetworkManager.Settings.Connection.Update", 0, settings,
		).Err; err != nil {
			return fmt.Errorf("update settings: %w", err)
		}

		// Fully bounce the connection so kernel addresses get reset
		return reloadConnection(conn, parts.SettingsPath)
	})
}

// --- IPv6 Configuration ---

func SetIPv6DHCP(iface string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("interface name is required")
	}

	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.ConnectSystemBus()
		if err != nil {
			return fmt.Errorf("connect system bus: %w", err)
		}
		defer conn.Close()

		parts, err := getActiveConnForIface(conn, iface)
		if err != nil {
			return err
		}

		settingsConn := conn.Object("org.freedesktop.NetworkManager", parts.SettingsPath)
		var settings map[string]map[string]godbus.Variant
		if err := settingsConn.Call(
			"org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0,
		).Store(&settings); err != nil {
			return fmt.Errorf("GetSettings: %w", err)
		}

		ip6 := settings["ipv6"]
		if ip6 == nil {
			ip6 = make(map[string]godbus.Variant)
		}

		ip6["method"] = godbus.MakeVariant("auto")
		delete(ip6, "addresses")
		delete(ip6, "address-data")
		delete(ip6, "routes")
		delete(ip6, "route-data")
		delete(ip6, "gateway")
		delete(ip6, "dns")
		delete(ip6, "dns-search")
		delete(ip6, "dns-options")
		delete(ip6, "never-default")
		delete(ip6, "may-fail")
		delete(ip6, "ignore-auto-dns")
		delete(ip6, "ignore-auto-routes")

		settings["ipv6"] = ip6

		if err := settingsConn.Call(
			"org.freedesktop.NetworkManager.Settings.Connection.Update", 0, settings,
		).Err; err != nil {
			return fmt.Errorf("update IPv6 DHCP settings: %w", err)
		}

		return reloadConnection(conn, parts.SettingsPath)
	})
}

func SetIPv6Static(iface, addressCIDR string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("interface name is required")
	}
	if strings.TrimSpace(addressCIDR) == "" {
		return fmt.Errorf("IPv6 CIDR is required")
	}

	addr, ipNet, err := stdnet.ParseCIDR(strings.TrimSpace(addressCIDR))
	if err != nil {
		return fmt.Errorf("invalid IPv6 CIDR %q: %w", addressCIDR, err)
	}
	if addr.To16() == nil || addr.To4() != nil {
		return fmt.Errorf("invalid IPv6 address %q", addressCIDR)
	}

	prefix, bits := ipNet.Mask.Size()
	if bits != 128 || prefix < 0 {
		return fmt.Errorf("invalid IPv6 prefix in %q", addressCIDR)
	}

	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.ConnectSystemBus()
		if err != nil {
			return fmt.Errorf("connect system bus: %w", err)
		}
		defer conn.Close()

		parts, err := getActiveConnForIface(conn, iface)
		if err != nil {
			return err
		}

		settingsConn := conn.Object("org.freedesktop.NetworkManager", parts.SettingsPath)
		var settings map[string]map[string]godbus.Variant
		if err := settingsConn.Call(
			"org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0,
		).Store(&settings); err != nil {
			return fmt.Errorf("GetSettings: %w", err)
		}

		ip6 := settings["ipv6"]
		if ip6 == nil {
			ip6 = make(map[string]godbus.Variant)
		}

		addrDict := map[string]godbus.Variant{
			"address": godbus.MakeVariant(addr.String()),
			"prefix":  godbus.MakeVariant(uint32(prefix)),
		}
		ip6["method"] = godbus.MakeVariant("manual")
		ip6["address-data"] = godbus.MakeVariant([]map[string]godbus.Variant{addrDict})
		delete(ip6, "addresses")
		delete(ip6, "routes")
		delete(ip6, "route-data")

		settings["ipv6"] = ip6

		if err := settingsConn.Call(
			"org.freedesktop.NetworkManager.Settings.Connection.Update", 0, settings,
		).Err; err != nil {
			return fmt.Errorf("update IPv6 static settings: %w", err)
		}

		return reloadConnection(conn, parts.SettingsPath)
	})
}

// --- Other Network Configuration ---

// --- Connection Enable/Disable ---

// DisableConnection disconnects the network interface
func DisableConnection(iface string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("interface name is required")
	}

	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.ConnectSystemBus()
		if err != nil {
			return fmt.Errorf("connect system bus: %w", err)
		}
		defer conn.Close()

		nm := conn.Object("org.freedesktop.NetworkManager", "/org/freedesktop/NetworkManager")

		// Find the device by interface name
		var devicePaths []godbus.ObjectPath
		if err := nm.Call("org.freedesktop.NetworkManager.GetDevices", 0).Store(&devicePaths); err != nil {
			return fmt.Errorf("GetDevices failed: %w", err)
		}

		for _, devPath := range devicePaths {
			dev := conn.Object("org.freedesktop.NetworkManager", devPath)
			var devIface string
			if err := dev.Call("org.freedesktop.DBus.Properties.Get", 0,
				"org.freedesktop.NetworkManager.Device", "Interface").Store(&devIface); err != nil {
				continue
			}
			if devIface != iface {
				continue
			}

			// Disconnect the device
			if err := dev.Call("org.freedesktop.NetworkManager.Device.Disconnect", 0).Err; err != nil {
				return fmt.Errorf("failed to disconnect device: %w", err)
			}
			return nil
		}

		return fmt.Errorf("interface %s not found", iface)
	})
}

// EnableConnection activates the network connection for the interface
func EnableConnection(iface string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("interface name is required")
	}

	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.ConnectSystemBus()
		if err != nil {
			return fmt.Errorf("connect system bus: %w", err)
		}
		defer conn.Close()

		nm := conn.Object("org.freedesktop.NetworkManager", "/org/freedesktop/NetworkManager")
		devicePath, err := findDevicePathByInterface(conn, nm, iface)
		if err != nil {
			return err
		}
		if devicePath == "" {
			return fmt.Errorf("interface %s not found", iface)
		}

		matchingConnPath, err := findConnectionProfileByInterface(conn, iface)
		if err != nil {
			return err
		}
		if matchingConnPath == "" {
			return fmt.Errorf("no connection profile found for interface %s", iface)
		}

		var specificObject godbus.ObjectPath = "/"
		if err := nm.Call("org.freedesktop.NetworkManager.ActivateConnection", 0,
			matchingConnPath, devicePath, specificObject).Err; err != nil {
			return fmt.Errorf("failed to activate connection: %w", err)
		}

		return nil
	})
}

func findDevicePathByInterface(conn *godbus.Conn, nm godbus.BusObject, iface string) (godbus.ObjectPath, error) {
	var devicePaths []godbus.ObjectPath
	if err := nm.Call("org.freedesktop.NetworkManager.GetDevices", 0).Store(&devicePaths); err != nil {
		return "", fmt.Errorf("GetDevices failed: %w", err)
	}
	for _, devPath := range devicePaths {
		dev := conn.Object("org.freedesktop.NetworkManager", devPath)
		var devIface string
		if err := dev.Call("org.freedesktop.DBus.Properties.Get", 0,
			"org.freedesktop.NetworkManager.Device", "Interface").Store(&devIface); err != nil {
			continue
		}
		if devIface == iface {
			return devPath, nil
		}
	}
	return "", nil
}

func findConnectionProfileByInterface(conn *godbus.Conn, iface string) (godbus.ObjectPath, error) {
	settings := conn.Object("org.freedesktop.NetworkManager", "/org/freedesktop/NetworkManager/Settings")
	var connPaths []godbus.ObjectPath
	if err := settings.Call("org.freedesktop.NetworkManager.Settings.ListConnections", 0).Store(&connPaths); err != nil {
		return "", fmt.Errorf("ListConnections failed: %w", err)
	}
	for _, connPath := range connPaths {
		settingsConn := conn.Object("org.freedesktop.NetworkManager", connPath)
		var connSettings map[string]map[string]godbus.Variant
		if err := settingsConn.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0).Store(&connSettings); err != nil {
			continue
		}
		if connection := connSettings["connection"]; connection != nil {
			if ifaceVariant, ok := connection["interface-name"]; ok {
				if connIface, ok := ifaceVariant.Value().(string); ok && connIface == iface {
					return connPath, nil
				}
			}
		}
	}
	return "", nil
}

func SetMTU(iface, mtu string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	if strings.TrimSpace(iface) == "" || strings.TrimSpace(mtu) == "" {
		return fmt.Errorf("SetMTU requires interface and MTU value")
	}

	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.ConnectSystemBus()
		if err != nil {
			return fmt.Errorf("failed to connect to system bus: %w", err)
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil {
				logger.Warnf("failed to close D-Bus connection: %v", cerr)
			}
		}()

		nm := conn.Object("org.freedesktop.NetworkManager", "/org/freedesktop/NetworkManager")

		var devicePaths []godbus.ObjectPath
		if err := nm.Call("org.freedesktop.NetworkManager.GetDevices", 0).Store(&devicePaths); err != nil {
			return fmt.Errorf("GetDevices failed: %w", err)
		}

		for _, devPath := range devicePaths {
			dev := conn.Object("org.freedesktop.NetworkManager", devPath)
			var devIface string
			if err := dev.Call("org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.NetworkManager.Device", "Interface").Store(&devIface); err != nil {
				continue
			}
			if devIface != iface {
				continue
			}

			var activeConn godbus.ObjectPath
			if err := dev.Call("org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.NetworkManager.Device", "ActiveConnection").Store(&activeConn); err != nil {
				return fmt.Errorf("failed to get ActiveConnection: %w", err)
			}

			ac := conn.Object("org.freedesktop.NetworkManager", activeConn)
			var connPath godbus.ObjectPath
			if err := ac.Call("org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.NetworkManager.Connection.Active", "Connection").Store(&connPath); err != nil {
				return fmt.Errorf("failed to get Connection path: %w", err)
			}

			settingsConn := conn.Object("org.freedesktop.NetworkManager", connPath)
			var settings map[string]map[string]godbus.Variant
			if err := settingsConn.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0).Store(&settings); err != nil {
				return fmt.Errorf("failed to get connection settings: %w", err)
			}

			mtuValue, err := strconv.ParseUint(mtu, 10, 32)
			if err != nil {
				return fmt.Errorf("invalid MTU value: %w", err)
			}

			ethernetSettings := settings["802-3-ethernet"]
			ethernetSettings["mtu"] = godbus.MakeVariant(uint32(mtuValue))
			settings["802-3-ethernet"] = ethernetSettings

			if err := settingsConn.Call("org.freedesktop.NetworkManager.Settings.Connection.Update", 0, settings).Err; err != nil {
				return fmt.Errorf("failed to update MTU: %w", err)
			}

			return reloadConnection(conn, connPath)
		}

		return fmt.Errorf("interface %s not found", iface)
	})
}
