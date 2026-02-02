package dbus

import (
	"fmt"
	"os"
	"os/exec"
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
	var results []NMInterfaceInfo

	snapshots, _ := net.IOCounters(true)
	snapshotMap := make(map[string]net.IOCountersStat)
	for _, s := range snapshots {
		snapshotMap[s.Name] = s
	}

	// Calculate interval once for all interfaces
	now := time.Now().Unix()
	interval := max(now-lastTimestamp, 1)

	var opErr error
	err := RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.SystemBus()
		if err != nil {
			return fmt.Errorf("failed to connect to system bus: %w", err)
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil && opErr == nil {
				opErr = cerr
			}
		}()

		nm := conn.Object("org.freedesktop.NetworkManager", "/org/freedesktop/NetworkManager")

		var devicePaths []godbus.ObjectPath
		if err := nm.Call("org.freedesktop.NetworkManager.GetDevices", 0).Store(&devicePaths); err != nil {
			return fmt.Errorf("GetDevices failed: %w", err)
		}

		for _, devPath := range devicePaths {
			dev := conn.Object("org.freedesktop.NetworkManager", devPath)

			props := make(map[string]godbus.Variant)
			if err := dev.Call("org.freedesktop.DBus.Properties.GetAll", 0, "org.freedesktop.NetworkManager.Device").Store(&props); err != nil {
				continue
			}

			name, _ := props["Interface"].Value().(string)
			mac, _ := props["HwAddress"].Value().(string)

			devType := uint32(0)
			if v, ok := props["DeviceType"]; ok {
				if cast, ok := v.Value().(uint32); ok {
					devType = cast
				}
			}
			ifaceType := mapDeviceType(devType)

			mtu := uint32(0)
			if v, ok := props["Mtu"]; ok && v.Value() != nil {
				if cast, ok := v.Value().(uint32); ok {
					mtu = cast
				}
			}

			state := uint32(0)
			if v, ok := props["State"]; ok && v.Value() != nil {
				if cast, ok := v.Value().(uint32); ok {
					state = cast
				}
			}

			speed := "unknown"
			duplex := "unknown"
			if name != "" {
				speedPath := fmt.Sprintf("/sys/class/net/%s/speed", name)
				duplexPath := fmt.Sprintf("/sys/class/net/%s/duplex", name)

				if b, err := os.ReadFile(speedPath); err == nil {
					speed = strings.TrimSpace(string(b)) + " Mbps"
				}
				if b, err := os.ReadFile(duplexPath); err == nil {
					duplex = strings.TrimSpace(string(b))
				}
			}

			var ip4s, ip6s, dns []string
			gateway := ""

			if ip4Path, ok := props["Ip4Config"].Value().(godbus.ObjectPath); ok && ip4Path != "/" {
				ip4Obj := conn.Object("org.freedesktop.NetworkManager", ip4Path)
				var ip4Props map[string]godbus.Variant
				if err := ip4Obj.Call("org.freedesktop.DBus.Properties.GetAll", 0, "org.freedesktop.NetworkManager.IP4Config").Store(&ip4Props); err == nil {
					if addresses, ok := ip4Props["Addresses"].Value().([][]uint32); ok {
						for _, addr := range addresses {
							ip := fmt.Sprintf("%d.%d.%d.%d/%d",
								byte(addr[0]), byte(addr[0]>>8), byte(addr[0]>>16), byte(addr[0]>>24), addr[1])
							ip4s = append(ip4s, ip)
						}
					}
					if dnsAddrs, ok := ip4Props["Nameservers"].Value().([]uint32); ok {
						for _, ip := range dnsAddrs {
							dns = append(dns, fmt.Sprintf("%d.%d.%d.%d", byte(ip), byte(ip>>8), byte(ip>>16), byte(ip>>24)))
						}
					}
					if gw, ok := ip4Props["Gateway"].Value().(string); ok {
						gateway = gw
					}
				}
			}

			if ip6Path, ok := props["Ip6Config"].Value().(godbus.ObjectPath); ok && ip6Path != "/" {
				ip6Obj := conn.Object("org.freedesktop.NetworkManager", ip6Path)
				var ip6Props map[string]godbus.Variant
				if err := ip6Obj.Call("org.freedesktop.DBus.Properties.GetAll", 0, "org.freedesktop.NetworkManager.IP6Config").Store(&ip6Props); err == nil {
					if addresses, ok := ip6Props["Addresses"].Value().([][]interface{}); ok {
						for _, tuple := range addresses {
							addrBytes, _ := tuple[0].([]byte)
							prefix, _ := tuple[1].(uint32)
							if len(addrBytes) == 16 {
								parts := make([]string, 8)
								for i := 0; i < 8; i++ {
									parts[i] = fmt.Sprintf("%02x%02x", addrBytes[2*i], addrBytes[2*i+1])
								}
								ip6s = append(ip6s, fmt.Sprintf("%s/%d", strings.Join(parts, ":"), prefix))
							}
						}
					}
					if dns6, ok := ip6Props["Nameservers"].Value().([][]byte); ok {
						for _, addr := range dns6 {
							if len(addr) == 16 {
								parts := make([]string, 8)
								for i := 0; i < 8; i++ {
									parts[i] = fmt.Sprintf("%02x%02x", addr[2*i], addr[2*i+1])
								}
								dns = append(dns, strings.Join(parts, ":"))
							}
						}
					}
					if gw, ok := ip6Props["Gateway"].Value().(string); ok && gateway == "" {
						gateway = gw
					}
				}
			}

			rxSpeed := 0.0
			txSpeed := 0.0
			if snapshot, ok := snapshotMap[name]; ok {
				if prev, ok := lastNetStats[name]; ok {
					rxSpeed = float64(snapshot.BytesRecv-prev.BytesRecv) / float64(interval)
					txSpeed = float64(snapshot.BytesSent-prev.BytesSent) / float64(interval)
				}
				lastNetStats[name] = snapshot
			}

			ipv4Method := getIPv4Method(conn, name)

			results = append(results, NMInterfaceInfo{
				Name:         name,
				Type:         ifaceType,
				MAC:          mac,
				MTU:          mtu,
				Speed:        speed,
				Duplex:       duplex,
				State:        state,
				IP4Addresses: ip4s,
				IP6Addresses: ip6s,
				RxSpeed:      rxSpeed,
				TxSpeed:      txSpeed,
				DNS:          dns,
				Gateway:      gateway,
				IPv4Method:   ipv4Method,
			})
		}
		return nil
	})

	// Update timestamp after processing all interfaces
	lastTimestamp = now

	return results, err
}

// --- IPv4 Configuration ---

// SetIPv4Manual configures static IPv4 with IP address, gateway, and DNS in one atomic operation
func SetIPv4Manual(iface, addressCIDR, gateway string, dnsServers []string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	// ---- validate
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

	// ---- parse DNS -> uint32[]
	dnsUint32 := make([]uint32, 0, len(dnsServers))
	for _, dnsStr := range dnsServers {
		dnsParts := strings.Split(strings.TrimSpace(dnsStr), ".")
		if len(dnsParts) != 4 {
			return fmt.Errorf("invalid DNS server format: %s", dnsStr)
		}
		var dnsVal uint32
		for i, part := range dnsParts {
			num, err := strconv.ParseUint(part, 10, 8)
			if err != nil {
				return fmt.Errorf("invalid DNS server: %s", dnsStr)
			}
			dnsVal |= uint32(num) << (uint(i) * 8)
		}
		dnsUint32 = append(dnsUint32, dnsVal)
	}

	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.SystemBus()
		if err != nil {
			return fmt.Errorf("connect system bus: %w", err)
		}
		defer conn.Close()

		parts, err := getActiveConnForIface(conn, iface)
		if err != nil {
			return err
		}

		// ---- parse CIDR
		s := strings.SplitN(strings.TrimSpace(addressCIDR), "/", 2)
		if len(s) != 2 {
			return fmt.Errorf("invalid IPv4 CIDR: %q", addressCIDR)
		}
		ip := strings.TrimSpace(s[0])
		pfx64, err := strconv.ParseUint(s[1], 10, 32)
		if err != nil {
			return fmt.Errorf("invalid IPv4 prefix: %q", s[1])
		}
		prefix := uint32(pfx64)

		settingsConn := conn.Object("org.freedesktop.NetworkManager", parts.SettingsPath)
		nm := conn.Object("org.freedesktop.NetworkManager", "/org/freedesktop/NetworkManager")
		dev := conn.Object("org.freedesktop.NetworkManager", parts.DevicePath)

		// ---- Step 1: disable IPv4 to ensure DHCP is stopped
		var settings map[string]map[string]godbus.Variant
		if err := settingsConn.Call(
			"org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0,
		).Store(&settings); err != nil {
			return fmt.Errorf("GetSettings: %w", err)
		}

		// sanitize binary fields
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

		ip4 := settings["ipv4"]
		if ip4 == nil {
			ip4 = make(map[string]godbus.Variant)
		}
		ip4["method"] = godbus.MakeVariant("disabled")
		delete(ip4, "address-data")
		delete(ip4, "dns")
		delete(ip4, "gateway")
		delete(ip4, "dhcp-client-id")
		delete(ip4, "dhcp-timeout")
		settings["ipv4"] = ip4

		if err := settingsConn.Call(
			"org.freedesktop.NetworkManager.Settings.Connection.Update", 0, settings,
		).Err; err != nil {
			return fmt.Errorf("disable IPv4: %w", err)
		}

		// Politely tell NM to drop any active lease:
		// Device.Disconnect (stops carrier/lease) + DeactivateConnection
		if err := dev.Call("org.freedesktop.NetworkManager.Device.Disconnect", 0).Err; err != nil {
			// not fatal on some device types, but worth returning
			return fmt.Errorf("device disconnect: %w", err)
		}
		if parts.ActivePath != "" && parts.ActivePath != "/" {
			if err := nm.Call("org.freedesktop.NetworkManager.DeactivateConnection", 0, parts.ActivePath).Err; err != nil {
				return fmt.Errorf("deactivate connection: %w", err)
			}
		}

		// small settle delay
		time.Sleep(1500 * time.Millisecond)

		// ---- Step 2: set manual IPv4 (address, gateway, dns) in one go
		if err := settingsConn.Call(
			"org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0,
		).Store(&settings); err != nil {
			return fmt.Errorf("GetSettings (2nd): %w", err)
		}

		// sanitize again
		if ip4m := settings["ipv4"]; ip4m != nil {
			delete(ip4m, "addresses")
			delete(ip4m, "address-data")
			delete(ip4m, "routes")
			delete(ip4m, "route-data")
			settings["ipv4"] = ip4m
		}
		if ip6 := settings["ipv6"]; ip6 != nil {
			delete(ip6, "addresses")
			delete(ip6, "routes")
			delete(ip6, "route-data")
			settings["ipv6"] = ip6
		}

		ip4 = settings["ipv4"]
		if ip4 == nil {
			ip4 = make(map[string]godbus.Variant)
		}

		addrDict := map[string]godbus.Variant{
			"address": godbus.MakeVariant(ip),
			"prefix":  godbus.MakeVariant(prefix),
		}
		ip4["address-data"] = godbus.MakeVariant([]map[string]godbus.Variant{addrDict})
		ip4["dns"] = godbus.MakeVariant(dnsUint32)
		ip4["gateway"] = godbus.MakeVariant(gateway)
		ip4["method"] = godbus.MakeVariant("manual")
		ip4["may-fail"] = godbus.MakeVariant(false)
		ip4["ignore-auto-dns"] = godbus.MakeVariant(true)
		ip4["ignore-auto-routes"] = godbus.MakeVariant(true)
		ip4["never-default"] = godbus.MakeVariant(false)

		settings["ipv4"] = ip4

		if err := settingsConn.Call(
			"org.freedesktop.NetworkManager.Settings.Connection.Update", 0, settings,
		).Err; err != nil {
			return fmt.Errorf("set manual config: %w", err)
		}

		// ---- Step 3: reactivate with the new settings
		var specificObject godbus.ObjectPath = "/"
		if err := nm.Call(
			"org.freedesktop.NetworkManager.ActivateConnection", 0,
			parts.SettingsPath, parts.DevicePath, specificObject,
		).Err; err != nil {
			return fmt.Errorf("reactivate: %w", err)
		}

		return nil
	})
}

// SetIPv4DHCP switches interface to DHCP (auto) mode
func SetIPv4DHCP(iface string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.SystemBus()
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
	cmd := exec.Command("nmcli", "con", "mod", iface, "ipv6.method", "auto")
	if err := cmd.Run(); err != nil {
		return err
	}
	cmd = exec.Command("nmcli", "con", "up", iface)
	return cmd.Run()
}

func SetIPv6Static(iface, addressCIDR string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	cmd := exec.Command("nmcli", "con", "mod", iface, "ipv6.addresses", addressCIDR, "ipv6.method", "manual")
	if err := cmd.Run(); err != nil {
		return err
	}
	cmd = exec.Command("nmcli", "con", "up", iface)
	return cmd.Run()
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
		conn, err := godbus.SystemBus()
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
		conn, err := godbus.SystemBus()
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

		var devicePath godbus.ObjectPath
		for _, devPath := range devicePaths {
			dev := conn.Object("org.freedesktop.NetworkManager", devPath)
			var devIface string
			if err := dev.Call("org.freedesktop.DBus.Properties.Get", 0,
				"org.freedesktop.NetworkManager.Device", "Interface").Store(&devIface); err != nil {
				continue
			}
			if devIface == iface {
				devicePath = devPath
				break
			}
		}

		if devicePath == "" {
			return fmt.Errorf("interface %s not found", iface)
		}

		// Find connection profiles that match this interface
		settings := conn.Object("org.freedesktop.NetworkManager", "/org/freedesktop/NetworkManager/Settings")
		var connPaths []godbus.ObjectPath
		if err := settings.Call("org.freedesktop.NetworkManager.Settings.ListConnections", 0).Store(&connPaths); err != nil {
			return fmt.Errorf("ListConnections failed: %w", err)
		}

		var matchingConnPath godbus.ObjectPath
		for _, connPath := range connPaths {
			settingsConn := conn.Object("org.freedesktop.NetworkManager", connPath)
			var connSettings map[string]map[string]godbus.Variant
			if err := settingsConn.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0).Store(&connSettings); err != nil {
				continue
			}

			// Check if this connection is for our interface
			if connection, ok := connSettings["connection"]; ok {
				if ifaceVariant, ok := connection["interface-name"]; ok {
					if connIface, ok := ifaceVariant.Value().(string); ok && connIface == iface {
						matchingConnPath = connPath
						break
					}
				}
			}
		}

		if matchingConnPath == "" {
			return fmt.Errorf("no connection profile found for interface %s", iface)
		}

		// Activate the connection
		var specificObject godbus.ObjectPath = "/"
		if err := nm.Call("org.freedesktop.NetworkManager.ActivateConnection", 0,
			matchingConnPath, devicePath, specificObject).Err; err != nil {
			return fmt.Errorf("failed to activate connection: %w", err)
		}

		return nil
	})
}

func SetMTU(iface, mtu string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	if strings.TrimSpace(iface) == "" || strings.TrimSpace(mtu) == "" {
		return fmt.Errorf("SetMTU requires interface and MTU value")
	}

	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.SystemBus()
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
