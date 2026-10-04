package network

import (
	"context"
	"fmt"
	"log/slog"
	stdnet "net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/vishvananda/netlink"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	networkbackend "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/network/internal/network"
)

var (
	networkMutationMu sync.Mutex
	networkEnv        = networkbackend.DefaultEnvironment()
)

func GetNetworkInfo(ctx context.Context) ([]apischema.NetworkInterface, error) {
	ifaces, err := stdnet.Interfaces()
	if err != nil {
		return nil, err
	}
	sort.Slice(ifaces, func(i, j int) bool { return ifaces[i].Name < ifaces[j].Name })

	dns := readSystemNameservers()
	gateways := readDefaultGateways()
	results := make([]apischema.NetworkInterface, 0, len(ifaces))
	for _, iface := range ifaces {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info := liveInterfaceInfo(iface, dns, gateways[iface.Name])
		if cfg, ok, err := networkbackend.ReadConfigBestEffort(networkEnv, iface.Name); err == nil && ok {
			mergeConfiguredState(&info, cfg)
		} else if err != nil {
			slog.Debug("network config unavailable", "component", "dbus", "subsystem", "network", "interface", iface.Name, "error", err)
		}
		info.LogUnit = resolveNetworkLogUnit(info.ConfigBackend)
		results = append(results, info)
	}
	return results, nil
}

func GetBridgeOptions(ctx context.Context) (apischema.NetworkBridgeOptions, error) {
	options, err := networkbackend.GetBridgeOptions(ctx, networkEnv)
	if err != nil {
		return apischema.NetworkBridgeOptions{}, fmt.Errorf("get bridge options: %w", err)
	}
	return mapBridgeOptions(options), nil
}

func CreateBridge(ctx context.Context, req apischema.NetworkBridgeCreateRequest) (apischema.NetworkBridgeCreateResult, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return apischema.NetworkBridgeCreateResult{}, fmt.Errorf("bridge name is required")
	}
	member := strings.TrimSpace(req.Member)
	if member == "" {
		return apischema.NetworkBridgeCreateResult{}, fmt.Errorf("bridge member interface is required")
	}

	unlock, err := beginNetworkMutation(ctx)
	if err != nil {
		return apischema.NetworkBridgeCreateResult{}, err
	}
	defer unlock()

	result, err := networkbackend.CreateBridge(ctx, networkEnv, networkbackend.BridgePlan{
		Name:   name,
		Member: member,
	})
	if err != nil {
		return apischema.NetworkBridgeCreateResult{}, fmt.Errorf("create bridge %q over %q: %w", name, member, err)
	}
	return apischema.NetworkBridgeCreateResult{
		Name:    result.Name,
		Member:  result.Member,
		Backend: result.Backend,
	}, nil
}

func mapBridgeOptions(options networkbackend.BridgeOptions) apischema.NetworkBridgeOptions {
	result := apischema.NetworkBridgeOptions{
		Candidates: make([]apischema.NetworkBridgeCandidate, 0, len(options.Candidates)),
		Warnings:   options.Warnings,
	}
	for _, candidate := range options.Candidates {
		result.Candidates = append(result.Candidates, apischema.NetworkBridgeCandidate{
			Name:            candidate.Name,
			MAC:             candidate.MAC,
			Backend:         candidate.Backend,
			Eligible:        candidate.Eligible,
			Reasons:         candidate.Reasons,
			HandoffEligible: candidate.HandoffEligible,
			HandoffReasons:  candidate.HandoffReasons,
		})
	}
	return result
}

func SetIPv4Manual(ctx context.Context, iface, addressCIDR, gateway string, dnsServers, search []string) error {
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
	if err := validateDNS(dnsServers, search); err != nil {
		return err
	}
	unlock, err := beginNetworkMutation(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	backend, err := networkbackend.OpenBackend(networkEnv, iface)
	if err != nil {
		return err
	}
	if setter, ok := backend.(networkbackend.DNSOptionsSetter); ok {
		return setter.SetIPv4ManualWithSearch(ctx, addressCIDR, gateway, dnsServers, search)
	}
	if len(search) > 0 {
		return fmt.Errorf("%w: %s cannot set DNS search domains", networkbackend.ErrUnsupportedBackend, backend.Name())
	}
	return backend.SetIPv4Manual(ctx, addressCIDR, gateway, dnsServers)
}

// SetIPv4DHCP switches iface to DHCP. Non-empty dnsServers replace the IPv4
// servers DHCP offers.
func SetIPv4DHCP(ctx context.Context, iface string, dnsServers, search []string) error {
	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("interface name is required")
	}
	if err := validateDNS(dnsServers, search); err != nil {
		return err
	}
	unlock, err := beginNetworkMutation(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	backend, err := networkbackend.OpenBackend(networkEnv, iface)
	if err != nil {
		return err
	}
	if setter, ok := backend.(networkbackend.DNSOptionsSetter); ok {
		return setter.SetIPv4DHCPWithDNS(ctx, dnsServers, search)
	}
	if len(dnsServers) > 0 || len(search) > 0 {
		return fmt.Errorf("%w: %s cannot set DNS alongside DHCP", networkbackend.ErrUnsupportedBackend, backend.Name())
	}
	return backend.SetIPv4DHCP(ctx)
}

// splitList splits a comma- or whitespace-separated form value.
func splitList(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
}

func validateDNS(dnsServers, search []string) error {
	for _, dns := range dnsServers {
		if stdnet.ParseIP(dns) == nil {
			return fmt.Errorf("invalid DNS server %q", dns)
		}
	}
	for _, domain := range search {
		if !isDomainName(domain) {
			return fmt.Errorf("invalid search domain %q", domain)
		}
	}
	return nil
}

func isDomainName(name string) bool {
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	for label := range strings.SplitSeq(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

func SetIPv6DHCP(ctx context.Context, iface string) error {
	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("interface name is required")
	}
	unlock, err := beginNetworkMutation(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	backend, err := networkbackend.OpenBackend(networkEnv, iface)
	if err != nil {
		return err
	}
	return backend.SetIPv6DHCP(ctx)
}

func DisableConnection(ctx context.Context, iface string) error {
	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("interface name is required")
	}
	unlock, err := beginNetworkMutation(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	backend, err := networkbackend.OpenBackend(networkEnv, iface)
	if err != nil {
		return err
	}
	return backend.Disable(ctx)
}

func EnableConnection(ctx context.Context, iface string) error {
	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("interface name is required")
	}
	unlock, err := beginNetworkMutation(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	backend, err := networkbackend.OpenBackend(networkEnv, iface)
	if err != nil {
		return err
	}
	return backend.Enable(ctx)
}

func SetMTU(ctx context.Context, iface, mtu string) error {
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
	unlock, err := beginNetworkMutation(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	backend, err := networkbackend.OpenBackend(networkEnv, iface)
	if err != nil {
		return err
	}
	return backend.SetMTU(ctx, uint32(value))
}

func SetOptional(ctx context.Context, iface string, optional bool) error {
	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("interface name is required")
	}
	unlock, err := beginNetworkMutation(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	backend, err := networkbackend.OpenBackend(networkEnv, iface)
	if err != nil {
		return err
	}
	setter, ok := backend.(networkbackend.OptionalSetter)
	if !ok {
		return fmt.Errorf("%w: %s cannot mark %s optional", networkbackend.ErrUnsupportedBackend, backend.Name(), iface)
	}
	return setter.SetOptional(ctx, optional)
}

func beginNetworkMutation(ctx context.Context) (func(), error) {
	networkMutationMu.Lock()
	if err := ctx.Err(); err != nil {
		networkMutationMu.Unlock()
		return nil, err
	}
	return networkMutationMu.Unlock, nil
}

func liveInterfaceInfo(
	iface stdnet.Interface,
	defaultDNS []string,
	gateway string,
) apischema.NetworkInterface {
	addrs, _ := iface.Addrs()
	ip4s := collectIPv4Addresses(addrs)
	// Stays "unknown" unless an on-disk backend claims the interface; the
	// pointer is always set so the field is never silently absent.
	ipv4Method := "unknown"
	return apischema.NetworkInterface{
		Name:       iface.Name,
		Type:       interfaceType(iface.Name),
		MAC:        iface.HardwareAddr.String(),
		MTU:        iface.MTU,
		Speed:      networkInterfaceLinkSpeed(iface.Name),
		Duplex:     networkInterfaceDuplex(iface.Name),
		Driver:     networkInterfaceDriver(iface.Name),
		OperState:  networkInterfaceOperState(iface.Name),
		Carrier:    networkInterfaceCarrier(iface.Name),
		State:      int(interfaceState(iface)),
		IPv4:       ip4s,
		DNS:        append(make([]string, 0, len(defaultDNS)), defaultDNS...),
		Gateway:    gateway,
		IPv4Method: &ipv4Method,
	}
}

// Never nil: the wire contract types ipv4 as an array, and a disconnected
// interface with no addresses must serialise as [] rather than null.
func collectIPv4Addresses(addrs []stdnet.Addr) []string {
	ip4s := []string{}
	for _, addr := range addrs {
		value := addr.String()
		ip, _, err := stdnet.ParseCIDR(value)
		if err != nil || ip == nil {
			continue
		}
		if ip.To4() != nil {
			ip4s = append(ip4s, value)
		}
	}
	return ip4s
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

// The driver link only exists for interfaces backed by a device, so bridges,
// bonds, tunnels and veths report no driver rather than a placeholder.
func networkInterfaceDriver(name string) string {
	if name == "" {
		return ""
	}
	target, err := os.Readlink(fmt.Sprintf("/sys/class/net/%s/device/driver", name))
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

func networkInterfaceOperState(name string) string {
	state, err := readOperState(name)
	if err != nil || state == "" {
		return "unknown"
	}
	return state
}

// Reading carrier fails on a down interface (EINVAL) and the attribute is
// absent on virtual ones, so an unreadable value stays nil: "unknown", not
// "no link".
func networkInterfaceCarrier(name string) *bool {
	if name == "" {
		return nil
	}
	data, err := os.ReadFile(fmt.Sprintf("/sys/class/net/%s/carrier", name))
	if err != nil {
		return nil
	}
	switch strings.TrimSpace(string(data)) {
	case "1":
		carrier := true
		return &carrier
	case "0":
		carrier := false
		return &carrier
	}
	return nil
}

func mergeConfiguredState(info *apischema.NetworkInterface, cfg networkbackend.InterfaceConfig) {
	info.ConfigBackend = cfg.Backend
	info.Optional = cfg.Optional
	ignoreDHCPDNS := false
	if cfg.DNSOptions != nil {
		ignoreDHCPDNS = cfg.DNSOptions.IgnoreDHCP
		info.DNSOptions = &apischema.NetworkDNSOptions{
			Search:     append([]string{}, cfg.DNSOptions.Search...),
			IgnoreDHCP: cfg.DNSOptions.IgnoreDHCP,
		}
	}
	if strings.TrimSpace(cfg.IPv4Method) != "" {
		ipv4Method := cfg.IPv4Method
		info.IPv4Method = &ipv4Method
	}
	if cfg.IPv4Method == "manual" && len(cfg.IPv4Addresses) > 0 {
		info.IPv4 = append([]string(nil), cfg.IPv4Addresses...)
	}
	if len(info.IPv4) == 0 && len(cfg.IPv4Addresses) > 0 {
		info.IPv4 = append([]string(nil), cfg.IPv4Addresses...)
	}
	if (cfg.IPv4Method == "manual" || ignoreDHCPDNS) && len(cfg.DNS) > 0 {
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
