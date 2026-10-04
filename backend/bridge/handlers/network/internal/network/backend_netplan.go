package network

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

type netplanBackend struct {
	baseBackend
	kind string
}

func (b *netplanBackend) Name() string {
	return "netplan"
}

func detectNetplanBackend(env Environment, iface string) (ConfigBackend, error) {
	var matches []string
	var kinds []string
	for _, pattern := range []string{"*.yaml", "*.yml"} {
		paths, err := globSorted(filepath.Join(env.NetplanDir, pattern))
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			doc, err := loadNetplanDoc(raw)
			if err != nil {
				return nil, fmt.Errorf("parse netplan %s: %w", path, err)
			}
			if kind, ok := doc.findInterfaceKind(iface); ok {
				matches = append(matches, path)
				kinds = append(kinds, kind)
			}
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) > 1 {
		return nil, ambiguousf(iface, "netplan", matches)
	}
	return &netplanBackend{
		env: env, iface: iface, path: matches[0],
		kind: kinds[0],
	}, nil
}

func (b *netplanBackend) Read() (InterfaceConfig, error) {
	doc, err := b.load()
	if err != nil {
		return InterfaceConfig{}, err
	}
	ifaceMap, err := doc.interfaceMap(b.kind, b.iface)
	if err != nil {
		return InterfaceConfig{}, err
	}
	cfg := InterfaceConfig{
		Backend:       b.Name(),
		IPv4Addresses: filterAddressesByFamily(netplanAddresses(ifaceMap), 4),
		IPv6Addresses: filterAddressesByFamily(netplanAddresses(ifaceMap), 6),
		DNS:           netplanDNS(ifaceMap),
		Gateway:       netplanGateway(ifaceMap),
	}
	cfg.IPv4Method = netplanMethod(ifaceMap, 4, cfg.IPv4Addresses)
	cfg.IPv6Method = netplanMethod(ifaceMap, 6, cfg.IPv6Addresses)
	if mtu, ok := netplanUint(ifaceMap["mtu"]); ok {
		cfg.MTU = &mtu
	}
	optional, _ := ifaceMap["optional"].(bool)
	cfg.Optional = &optional
	overrides, _ := ifaceMap["dhcp4-overrides"].(map[string]any)
	useDNS, set := overrides["use-dns"].(bool)
	cfg.DNSOptions = &DNSOptions{
		Search:     netplanSearch(ifaceMap),
		IgnoreDHCP: set && !useDNS,
	}
	return cfg, nil
}

func (b *netplanBackend) SetIPv4DHCP(ctx context.Context) error {
	return b.SetIPv4DHCPWithDNS(ctx, nil, nil)
}

// SetIPv4DHCPWithDNS keeps DHCP for the address and gateway; non-empty dns
// replaces the DHCP-provided IPv4 servers instead of adding to them.
func (b *netplanBackend) SetIPv4DHCPWithDNS(ctx context.Context, dns, search []string) error {
	return b.update(ctx, func(ifaceMap map[string]any) error {
		ifaceMap["dhcp4"] = true
		ifaceMap["addresses"] = replaceNetplanAddresses(ifaceMap["addresses"], 4, nil)
		setNetplanGateway(ifaceMap, "")
		setNetplanDNS(ifaceMap, mergeDNSPreservingOtherFamily(netplanDNS(ifaceMap), dns, 4))
		setNetplanSearch(ifaceMap, search)
		setNetplanIgnoreDHCPDNS(ifaceMap, len(dns) > 0)
		return nil
	})
}

func (b *netplanBackend) SetIPv4Manual(ctx context.Context, addressCIDR, gateway string, dns []string) error {
	return b.SetIPv4ManualWithSearch(ctx, addressCIDR, gateway, dns, nil)
}

func (b *netplanBackend) SetIPv4ManualWithSearch(ctx context.Context, addressCIDR, gateway string, dns, search []string) error {
	if _, _, err := parseIPv4CIDR(addressCIDR); err != nil {
		return err
	}
	if !isIPv4(gateway) {
		return fmt.Errorf("invalid IPv4 gateway %q", gateway)
	}
	return b.update(ctx, func(ifaceMap map[string]any) error {
		ifaceMap["dhcp4"] = false
		ifaceMap["addresses"] = replaceNetplanAddresses(ifaceMap["addresses"], 4, []string{strings.TrimSpace(addressCIDR)})
		setNetplanGateway(ifaceMap, gateway)
		setNetplanDNS(ifaceMap, mergeDNSPreservingOtherFamily(netplanDNS(ifaceMap), dns, 4))
		setNetplanSearch(ifaceMap, search)
		setNetplanIgnoreDHCPDNS(ifaceMap, false)
		return nil
	})
}

func (b *netplanBackend) SetIPv6DHCP(ctx context.Context) error {
	return b.update(ctx, func(ifaceMap map[string]any) error {
		ifaceMap["dhcp6"] = true
		ifaceMap["addresses"] = replaceNetplanAddresses(ifaceMap["addresses"], 6, nil)
		return nil
	})
}

func (b *netplanBackend) SetIPv6Static(ctx context.Context, addressCIDR string) error {
	if _, _, err := parseIPv6CIDR(addressCIDR); err != nil {
		return err
	}
	return b.update(ctx, func(ifaceMap map[string]any) error {
		ifaceMap["dhcp6"] = false
		ifaceMap["addresses"] = replaceNetplanAddresses(ifaceMap["addresses"], 6, []string{strings.TrimSpace(addressCIDR)})
		return nil
	})
}

func (b *netplanBackend) SetMTU(ctx context.Context, mtu uint32) error {
	return b.update(ctx, func(ifaceMap map[string]any) error {
		ifaceMap["mtu"] = int64(mtu)
		return nil
	})
}

// SetOptional only affects whether boot waits for the link, so it regenerates
// the renderer config without an Apply that would bounce a working interface.
func (b *netplanBackend) SetOptional(ctx context.Context, optional bool) error {
	return b.write(ctx, func(ifaceMap map[string]any) error {
		if optional {
			ifaceMap["optional"] = true
		} else {
			delete(ifaceMap, "optional")
		}
		return nil
	})
}

func (b *netplanBackend) Enable(ctx context.Context) error {
	doc, err := b.load()
	if err != nil {
		return err
	}
	ifaceMap, err := doc.interfaceMap(b.kind, b.iface)
	if err != nil {
		return err
	}
	// Only touch the file when Disable left it off, so enabling a link does
	// not rewrite a hand-written config.
	if mode, _ := ifaceMap["activation-mode"].(string); mode == "off" {
		if err := b.write(ctx, func(ifaceMap map[string]any) error {
			delete(ifaceMap, "activation-mode")
			return nil
		}); err != nil {
			return err
		}
	}
	if err := b.apply(ctx); err != nil {
		return err
	}
	return setLinkUp(ctx, b.iface)
}

// Disable persists activation-mode: off so the link stays down after a
// reboot. Netplan older than 0.103 rejects the key; the link then still goes
// down, as before, until the next boot.
func (b *netplanBackend) Disable(ctx context.Context) error {
	if err := b.write(ctx, func(ifaceMap map[string]any) error {
		ifaceMap["activation-mode"] = "off"
		return nil
	}); err != nil {
		slog.Warn("netplan could not persist disabled link", "component", "dbus", "subsystem", "network", "interface", b.iface, "error", err)
	}
	return setLinkDown(ctx, b.iface)
}

func (b *netplanBackend) load() (*netplanDoc, error) {
	raw, err := os.ReadFile(b.path)
	if err != nil {
		return nil, err
	}
	doc, err := loadNetplanDoc(raw)
	if err != nil {
		return nil, err
	}
	if _, err := doc.interfaceMap(b.kind, b.iface); err != nil {
		return nil, err
	}
	return doc, nil
}

func (b *netplanBackend) update(ctx context.Context, updateFn func(ifaceMap map[string]any) error) error {
	if err := b.write(ctx, updateFn); err != nil {
		return err
	}
	return b.apply(ctx)
}

// write persists updateFn's change and regenerates, restoring the original
// file when netplan rejects it.
func (b *netplanBackend) write(ctx context.Context, updateFn func(ifaceMap map[string]any) error) error {
	original, err := os.ReadFile(b.path)
	if err != nil {
		return err
	}
	doc, err := loadNetplanDoc(original)
	if err != nil {
		return err
	}
	ifaceMap, err := doc.interfaceMap(b.kind, b.iface)
	if err != nil {
		return err
	}
	updateErr := updateFn(ifaceMap)
	if updateErr != nil {
		return updateErr
	}
	rendered, err := yaml.Marshal(doc.root)
	if err != nil {
		return err
	}
	mode := existingMode(b.path, 0o644)
	if err := b.env.WriteFile(b.path, rendered, mode); err != nil {
		return err
	}
	if err := b.generate(ctx); err != nil {
		_ = b.env.WriteFile(b.path, original, mode)
		return err
	}
	return nil
}

func (b *netplanBackend) generate(ctx context.Context) error {
	if err := b.env.NetplanCall(ctx, "Generate"); err != nil {
		return fmt.Errorf("netplan generate: %w", err)
	}
	return nil
}

func (b *netplanBackend) apply(ctx context.Context) error {
	if err := b.env.NetplanCall(ctx, "Apply"); err != nil {
		return fmt.Errorf("netplan apply: %w", err)
	}
	return nil
}

type netplanDoc struct {
	root map[string]any
}

func loadNetplanDoc(data []byte) (*netplanDoc, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return &netplanDoc{root: map[string]any{}}, nil
	}
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if root == nil {
		root = map[string]any{}
	}
	return &netplanDoc{root: root}, nil
}

func (d *netplanDoc) findInterfaceKind(iface string) (string, bool) {
	for _, kind := range []string{"ethernets", "wifis"} {
		if _, err := d.interfaceMap(kind, iface); err == nil {
			return kind, true
		}
	}
	return "", false
}

func (d *netplanDoc) interfaceMap(kind, iface string) (map[string]any, error) {
	networkMap := ensureMap(d.root, "network")
	kindMap := ensureMap(networkMap, kind)
	raw, ok := kindMap[iface]
	if !ok {
		return nil, fmt.Errorf("interface %s not declared in %s", iface, kind)
	}
	ifaceMap, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid netplan definition for interface %s", iface)
	}
	return ifaceMap, nil
}

func ensureMap(root map[string]any, key string) map[string]any {
	value, ok := root[key]
	if !ok {
		mapped := map[string]any{}
		root[key] = mapped
		return mapped
	}
	mapped, ok := value.(map[string]any)
	if !ok {
		mapped = map[string]any{}
		root[key] = mapped
	}
	return mapped
}

func netplanMethod(ifaceMap map[string]any, family int, addresses []string) string {
	dhcpKey := "dhcp4"
	if family == 6 {
		dhcpKey = "dhcp6"
	}
	if dhcp, ok := ifaceMap[dhcpKey].(bool); ok && dhcp {
		return "auto"
	}
	if len(addresses) > 0 {
		return "manual"
	}
	if dhcp, ok := ifaceMap[dhcpKey].(bool); ok && !dhcp {
		return "disabled"
	}
	return "unknown"
}

func netplanAddresses(ifaceMap map[string]any) []string {
	values, ok := ifaceMap["addresses"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if address, ok := value.(string); ok && strings.TrimSpace(address) != "" {
			out = append(out, strings.TrimSpace(address))
		}
	}
	return out
}

func replaceNetplanAddresses(raw any, family int, replacement []string) []string {
	return replaceFamilyAddresses(netplanAddresses(map[string]any{"addresses": raw}), family, replacement)
}

func netplanDNS(ifaceMap map[string]any) []string {
	nameservers, ok := ifaceMap["nameservers"].(map[string]any)
	if !ok {
		return nil
	}
	values, ok := nameservers["addresses"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if entry, ok := value.(string); ok && strings.TrimSpace(entry) != "" {
			out = append(out, strings.TrimSpace(entry))
		}
	}
	return out
}

func setNetplanDNS(ifaceMap map[string]any, dns []string) {
	nameservers, _ := ifaceMap["nameservers"].(map[string]any)
	if nameservers == nil {
		if len(dns) == 0 {
			return
		}
		nameservers = map[string]any{}
	}
	if len(dns) == 0 {
		delete(nameservers, "addresses")
		if len(nameservers) == 0 {
			delete(ifaceMap, "nameservers")
			return
		}
		ifaceMap["nameservers"] = nameservers
		return
	}
	values := make([]any, 0, len(dns))
	for _, entry := range dns {
		values = append(values, entry)
	}
	nameservers["addresses"] = values
	ifaceMap["nameservers"] = nameservers
}

func netplanSearch(ifaceMap map[string]any) []string {
	nameservers, _ := ifaceMap["nameservers"].(map[string]any)
	values, _ := nameservers["search"].([]any)
	out := make([]string, 0, len(values))
	for _, value := range values {
		if entry, ok := value.(string); ok && strings.TrimSpace(entry) != "" {
			out = append(out, strings.TrimSpace(entry))
		}
	}
	return out
}

func setNetplanSearch(ifaceMap map[string]any, search []string) {
	nameservers, _ := ifaceMap["nameservers"].(map[string]any)
	if len(search) == 0 {
		if nameservers == nil {
			return
		}
		delete(nameservers, "search")
		if len(nameservers) == 0 {
			delete(ifaceMap, "nameservers")
		}
		return
	}
	if nameservers == nil {
		nameservers = map[string]any{}
		ifaceMap["nameservers"] = nameservers
	}
	values := make([]any, 0, len(search))
	for _, entry := range search {
		values = append(values, entry)
	}
	nameservers["search"] = values
}

func setNetplanIgnoreDHCPDNS(ifaceMap map[string]any, ignore bool) {
	overrides, _ := ifaceMap["dhcp4-overrides"].(map[string]any)
	if ignore {
		if overrides == nil {
			overrides = map[string]any{}
			ifaceMap["dhcp4-overrides"] = overrides
		}
		overrides["use-dns"] = false
		return
	}
	if overrides == nil {
		return
	}
	delete(overrides, "use-dns")
	if len(overrides) == 0 {
		delete(ifaceMap, "dhcp4-overrides")
	}
}

func netplanGateway(ifaceMap map[string]any) string {
	if gateway, ok := ifaceMap["gateway4"].(string); ok {
		return strings.TrimSpace(gateway)
	}
	routes, ok := ifaceMap["routes"].([]any)
	if !ok {
		return ""
	}
	for _, entry := range routes {
		route, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		to, _ := route["to"].(string)
		via, _ := route["via"].(string)
		if (to == "default" || to == "0.0.0.0/0") && isIPv4(via) {
			return strings.TrimSpace(via)
		}
	}
	return ""
}

func setNetplanGateway(ifaceMap map[string]any, gateway string) {
	delete(ifaceMap, "gateway4")
	routes, _ := ifaceMap["routes"].([]any)
	filtered := make([]any, 0, len(routes)+1)
	for _, entry := range routes {
		route, ok := entry.(map[string]any)
		if !ok {
			filtered = append(filtered, entry)
			continue
		}
		to, _ := route["to"].(string)
		via, _ := route["via"].(string)
		if (to == "default" || to == "0.0.0.0/0") && isIPv4(via) {
			continue
		}
		filtered = append(filtered, entry)
	}
	if strings.TrimSpace(gateway) != "" {
		filtered = append(filtered, map[string]any{
			"to":  "default",
			"via": strings.TrimSpace(gateway),
		})
	}
	if len(filtered) == 0 {
		delete(ifaceMap, "routes")
		return
	}
	ifaceMap["routes"] = filtered
}

func netplanUint(value any) (uint32, bool) {
	switch typed := value.(type) {
	case int:
		return uint32(typed), true
	case int64:
		return uint32(typed), true
	case uint64:
		return uint32(typed), true
	case float64:
		return uint32(typed), true
	}
	return 0, false
}
