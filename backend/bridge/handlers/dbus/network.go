package dbus

import (
	"fmt"
	"log/slog"
	stdnet "net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/dbus/internal/network"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/vishvananda/netlink"
)

type NetworkInterfaceInfo struct {
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
	IPv4Method   string   `json:"ipv4_method"`
}

var (
	lastNetStats  = make(map[string]net.IOCountersStat)
	lastTimestamp int64
	networkEnv    = network.DefaultEnvironment()
)

func GetNetworkInfo() ([]NetworkInterfaceInfo, error) {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	snapshotMap, now, interval := currentNetworkSnapshot()
	defer func() { lastTimestamp = now }()

	ifaces, err := stdnet.Interfaces()
	if err != nil {
		return nil, err
	}
	sort.Slice(ifaces, func(i, j int) bool { return ifaces[i].Name < ifaces[j].Name })

	dns := readSystemNameservers()
	gateways := readDefaultGateways()
	results := make([]NetworkInterfaceInfo, 0, len(ifaces))
	for _, iface := range ifaces {
		info := liveInterfaceInfo(iface, dns, gateways[iface.Name], snapshotMap, interval)
		if cfg, ok, err := network.ReadConfigBestEffort(networkEnv, iface.Name); err == nil && ok {
			mergeConfiguredState(&info, cfg)
		} else if err != nil {
			slog.Debug("network config unavailable", "component", "dbus", "subsystem", "network", "interface", iface.Name, "error", err)
		}
		results = append(results, info)
	}
	return results, nil
}

func SetIPv4Manual(iface, addressCIDR, gateway string, dnsServers []string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

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
	backend, err := network.OpenBackend(networkEnv, iface)
	if err != nil {
		return err
	}
	return backend.SetIPv4Manual(addressCIDR, gateway, dnsServers)
}

func SetIPv4DHCP(iface string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("interface name is required")
	}
	backend, err := network.OpenBackend(networkEnv, iface)
	if err != nil {
		return err
	}
	return backend.SetIPv4DHCP()
}

func SetIPv6DHCP(iface string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("interface name is required")
	}
	backend, err := network.OpenBackend(networkEnv, iface)
	if err != nil {
		return err
	}
	return backend.SetIPv6DHCP()
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
	backend, err := network.OpenBackend(networkEnv, iface)
	if err != nil {
		return err
	}
	return backend.SetIPv6Static(addressCIDR)
}

func DisableConnection(iface string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("interface name is required")
	}
	backend, err := network.OpenBackend(networkEnv, iface)
	if err != nil {
		return err
	}
	return backend.Disable()
}

func EnableConnection(iface string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("interface name is required")
	}
	backend, err := network.OpenBackend(networkEnv, iface)
	if err != nil {
		return err
	}
	return backend.Enable()
}

func SetMTU(iface, mtu string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	if strings.TrimSpace(iface) == "" || strings.TrimSpace(mtu) == "" {
		return fmt.Errorf("SetMTU requires interface and MTU value")
	}
	value, err := strconv.ParseUint(strings.TrimSpace(mtu), 10, 16)
	if err != nil {
		return fmt.Errorf("invalid MTU value: %w", err)
	}
	if value < 68 {
		return fmt.Errorf("invalid MTU value: %d (must be between 68 and 65535)", value)
	}
	backend, err := network.OpenBackend(networkEnv, iface)
	if err != nil {
		return err
	}
	return backend.SetMTU(uint32(value))
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

func liveInterfaceInfo(
	iface stdnet.Interface,
	defaultDNS []string,
	gateway string,
	snapshotMap map[string]net.IOCountersStat,
	interval int64,
) NetworkInterfaceInfo {
	addrs, _ := iface.Addrs()
	ip4s, ip6s := collectAddresses(addrs)
	mtu := uint32(0)
	if iface.MTU > 0 {
		mtu = uint32(iface.MTU)
	}
	rxSpeed, txSpeed := networkInterfaceSpeed(iface.Name, snapshotMap, interval)
	return NetworkInterfaceInfo{
		Name:         iface.Name,
		Type:         interfaceType(iface.Name),
		MAC:          iface.HardwareAddr.String(),
		MTU:          mtu,
		Speed:        networkInterfaceLinkSpeed(iface.Name),
		Duplex:       networkInterfaceDuplex(iface.Name),
		State:        interfaceState(iface),
		IP4Addresses: ip4s,
		IP6Addresses: ip6s,
		RxSpeed:      rxSpeed,
		TxSpeed:      txSpeed,
		DNS:          append([]string(nil), defaultDNS...),
		Gateway:      gateway,
		IPv4Method:   "unknown",
	}
}

func collectAddresses(addrs []stdnet.Addr) ([]string, []string) {
	var ip4s []string
	var ip6s []string
	for _, addr := range addrs {
		value := addr.String()
		ip, _, err := stdnet.ParseCIDR(value)
		if err != nil || ip == nil {
			continue
		}
		if ip.To4() != nil {
			ip4s = append(ip4s, value)
			continue
		}
		ip6s = append(ip6s, value)
	}
	return ip4s, ip6s
}

func interfaceType(name string) string {
	switch {
	case strings.HasPrefix(name, "lo"):
		return "loopback"
	case strings.HasPrefix(name, "wl"):
		return "wifi"
	default:
		return "ethernet"
	}
}

func interfaceState(iface stdnet.Interface) uint32 {
	if iface.Flags&stdnet.FlagUp == 0 {
		return 20
	}
	if data, err := stdnet.InterfaceByName(iface.Name); err == nil && data.Flags&stdnet.FlagRunning != 0 {
		return 100
	}
	if operstate, err := readOperState(iface.Name); err == nil {
		switch operstate {
		case "up", "unknown":
			return 100
		case "dormant", "lowerlayerdown":
			return 50
		}
	}
	return 100
}

func readOperState(name string) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/sys/class/net/%s/operstate", name))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func readSystemNameservers() []string {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	var servers []string
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 2 && fields[0] == "nameserver" {
			servers = append(servers, fields[1])
		}
	}
	return servers
}

func readDefaultGateways() map[string]string {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return map[string]string{}
	}
	gateways := make(map[string]string)
	for _, route := range routes {
		if route.Gw == nil || route.LinkIndex == 0 {
			continue
		}
		if route.Dst != nil {
			ones, bits := route.Dst.Mask.Size()
			if bits != 32 || ones != 0 {
				continue
			}
		}
		link, err := netlink.LinkByIndex(route.LinkIndex)
		if err != nil {
			continue
		}
		gateways[link.Attrs().Name] = route.Gw.String()
	}
	return gateways
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

func mergeConfiguredState(info *NetworkInterfaceInfo, cfg network.InterfaceConfig) {
	info.IPv4Method = cfg.IPv4Method
	if cfg.MTU != nil {
		info.MTU = *cfg.MTU
	}
	if cfg.IPv4Method == "manual" && len(cfg.IPv4Addresses) > 0 {
		info.IP4Addresses = append([]string(nil), cfg.IPv4Addresses...)
	}
	if len(info.IP4Addresses) == 0 && len(cfg.IPv4Addresses) > 0 {
		info.IP4Addresses = append([]string(nil), cfg.IPv4Addresses...)
	}
	if len(info.IP6Addresses) == 0 && len(cfg.IPv6Addresses) > 0 {
		info.IP6Addresses = append([]string(nil), cfg.IPv6Addresses...)
	}
	if cfg.IPv4Method == "manual" && len(cfg.DNS) > 0 {
		info.DNS = append([]string(nil), cfg.DNS...)
	} else if len(info.DNS) == 0 && len(cfg.DNS) > 0 {
		info.DNS = append([]string(nil), cfg.DNS...)
	}
	if cfg.IPv4Method == "manual" && strings.TrimSpace(cfg.Gateway) != "" {
		info.Gateway = cfg.Gateway
	} else if strings.TrimSpace(info.Gateway) == "" && strings.TrimSpace(cfg.Gateway) != "" {
		info.Gateway = cfg.Gateway
	}
}
