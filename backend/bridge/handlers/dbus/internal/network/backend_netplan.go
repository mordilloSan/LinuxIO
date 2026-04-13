package network

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type netplanBackend struct {
	baseBackend
	kind string
}

func (b *netplanBackend) Name() string {
	return "netplan"
}

func detectNetplanBackend(env Environment, iface string) (Backend, error) {
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
		baseBackend: baseBackend{env: env, iface: iface, path: matches[0]},
		kind:        kinds[0],
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
	return cfg, nil
}

func (b *netplanBackend) SetIPv4DHCP() error {
	return b.update(func(ifaceMap map[string]any) error {
		ifaceMap["dhcp4"] = true
		ifaceMap["addresses"] = replaceNetplanAddresses(ifaceMap["addresses"], 4, nil)
		setNetplanGateway(ifaceMap, "")
		setNetplanDNS(ifaceMap, mergeDNSPreservingOtherFamily(netplanDNS(ifaceMap), nil, 4))
		return nil
	})
}

func (b *netplanBackend) SetIPv4Manual(addressCIDR, gateway string, dns []string) error {
	if _, _, err := parseIPv4CIDR(addressCIDR); err != nil {
		return err
	}
	if !isIPv4(gateway) {
		return fmt.Errorf("invalid IPv4 gateway %q", gateway)
	}
	return b.update(func(ifaceMap map[string]any) error {
		ifaceMap["dhcp4"] = false
		ifaceMap["addresses"] = replaceNetplanAddresses(ifaceMap["addresses"], 4, []string{strings.TrimSpace(addressCIDR)})
		setNetplanGateway(ifaceMap, gateway)
		setNetplanDNS(ifaceMap, mergeDNSPreservingOtherFamily(netplanDNS(ifaceMap), dns, 4))
		return nil
	})
}

func (b *netplanBackend) SetIPv6DHCP() error {
	return b.update(func(ifaceMap map[string]any) error {
		ifaceMap["dhcp6"] = true
		ifaceMap["addresses"] = replaceNetplanAddresses(ifaceMap["addresses"], 6, nil)
		return nil
	})
}

func (b *netplanBackend) SetIPv6Static(addressCIDR string) error {
	if _, _, err := parseIPv6CIDR(addressCIDR); err != nil {
		return err
	}
	return b.update(func(ifaceMap map[string]any) error {
		ifaceMap["dhcp6"] = false
		ifaceMap["addresses"] = replaceNetplanAddresses(ifaceMap["addresses"], 6, []string{strings.TrimSpace(addressCIDR)})
		return nil
	})
}

func (b *netplanBackend) SetMTU(mtu uint32) error {
	return b.update(func(ifaceMap map[string]any) error {
		ifaceMap["mtu"] = int64(mtu)
		return nil
	})
}

func (b *netplanBackend) Enable() error {
	if err := b.apply(); err != nil {
		return err
	}
	return setLinkUp(b.iface)
}

func (b *netplanBackend) Disable() error {
	return setLinkDown(b.iface)
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

func (b *netplanBackend) update(updateFn func(ifaceMap map[string]any) error) error {
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
	rendered, err := doc.render()
	if err != nil {
		return err
	}
	mode := existingMode(b.path, 0o644)
	if err := b.env.WriteFile(b.path, rendered, mode); err != nil {
		return err
	}
	if err := b.generate(); err != nil {
		_ = b.env.WriteFile(b.path, original, mode)
		return err
	}
	return b.apply()
}

func (b *netplanBackend) generate() error {
	output, err := b.env.Runner.Run("netplan", "generate")
	return commandError("netplan", []string{"generate"}, output, err)
}

func (b *netplanBackend) apply() error {
	output, err := b.env.Runner.Run("netplan", "apply")
	return commandError("netplan", []string{"apply"}, output, err)
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

func (d *netplanDoc) render() ([]byte, error) {
	return yaml.Marshal(d.root)
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
