package network

import (
	"fmt"
	"path/filepath"
	"strings"

	ini "gopkg.in/ini.v1"
)

type networkdBackend struct {
	baseBackend
}

func (b *networkdBackend) Name() string {
	return "systemd-networkd"
}

func detectNetworkdBackend(env Environment, iface string) (Backend, error) {
	paths, err := globSorted(filepath.Join(env.NetworkdDir, "*.network"))
	if err != nil {
		return nil, err
	}
	var matches []string
	for _, path := range paths {
		cfg, err := readINIFile(path)
		if err != nil {
			return nil, fmt.Errorf("parse networkd file %s: %w", path, err)
		}
		if networkdMatchesInterface(cfg, iface) {
			matches = append(matches, path)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) > 1 {
		return nil, ambiguousf(iface, "systemd-networkd", matches)
	}
	return &networkdBackend{baseBackend: baseBackend{env: env, iface: iface, path: matches[0]}}, nil
}

func (b *networkdBackend) Read() (InterfaceConfig, error) {
	cfg, err := readINIFile(b.path)
	if err != nil {
		return InterfaceConfig{}, err
	}
	networkSection := cfg.Section("Network")
	addresses := append(sectionShadowValues(networkSection, "Address"), sectionShadowValues(cfg.Section("Address"), "Address")...)
	dns := append(sectionShadowValues(networkSection, "DNS"), sectionShadowValues(cfg.Section("Address"), "DNS")...)
	gateways := append(sectionShadowValues(networkSection, "Gateway"), sectionShadowValues(cfg.Section("Route"), "Gateway")...)
	result := InterfaceConfig{
		Backend:       b.Name(),
		IPv4Addresses: filterAddressesByFamily(addresses, 4),
		IPv6Addresses: filterAddressesByFamily(addresses, 6),
		DNS:           dns,
		Gateway:       firstIPv4(gateways),
	}
	dhcp4, dhcp6 := parseNetworkdDHCP(networkSection.Key("DHCP").String())
	result.IPv4Method = networkdMethod(dhcp4, result.IPv4Addresses)
	result.IPv6Method = networkdMethod(dhcp6, result.IPv6Addresses)
	if linkSection, err := cfg.GetSection("Link"); err == nil {
		if mtu := strings.TrimSpace(linkSection.Key("MTUBytes").String()); mtu != "" {
			value, err := parseMTU(mtu)
			if err == nil {
				result.MTU = &value
			}
		}
	}
	return result, nil
}

func (b *networkdBackend) SetIPv4DHCP() error {
	return b.update(func(cfg *ini.File) error {
		return updateNetworkdConfig(cfg, 4, "", "", nil, nil)
	})
}

func (b *networkdBackend) SetIPv4Manual(addressCIDR, gateway string, dns []string) error {
	if _, _, err := parseIPv4CIDR(addressCIDR); err != nil {
		return err
	}
	if !isIPv4(gateway) {
		return fmt.Errorf("invalid IPv4 gateway %q", gateway)
	}
	return b.update(func(cfg *ini.File) error {
		return updateNetworkdConfig(cfg, 4, addressCIDR, gateway, dns, nil)
	})
}

func (b *networkdBackend) SetIPv6DHCP() error {
	return b.update(func(cfg *ini.File) error {
		return updateNetworkdConfig(cfg, 6, "", "", nil, nil)
	})
}

func (b *networkdBackend) SetIPv6Static(addressCIDR string) error {
	if _, _, err := parseIPv6CIDR(addressCIDR); err != nil {
		return err
	}
	return b.update(func(cfg *ini.File) error {
		return updateNetworkdConfig(cfg, 6, addressCIDR, "", nil, nil)
	})
}

func (b *networkdBackend) SetMTU(mtu uint32) error {
	return b.update(func(cfg *ini.File) error {
		cfg.Section("Link").Key("MTUBytes").SetValue(fmt.Sprintf("%d", mtu))
		return nil
	})
}

func (b *networkdBackend) Enable() error {
	if err := b.reloadAndReconfigure(); err != nil {
		return err
	}
	output, err := b.env.Runner.Run("networkctl", "up", b.iface)
	if err != nil {
		return commandError("networkctl", []string{"up", b.iface}, output, err)
	}
	return nil
}

func (b *networkdBackend) Disable() error {
	output, err := b.env.Runner.Run("networkctl", "down", b.iface)
	return commandError("networkctl", []string{"down", b.iface}, output, err)
}

func (b *networkdBackend) update(updateFn func(cfg *ini.File) error) error {
	cfg, err := readINIFile(b.path)
	if err != nil {
		return err
	}
	layoutErr := ensureSimpleNetworkdLayout(cfg)
	if layoutErr != nil {
		return layoutErr
	}
	updateErr := updateFn(cfg)
	if updateErr != nil {
		return updateErr
	}
	rendered, err := renderINI(cfg)
	if err != nil {
		return err
	}
	if err := b.env.WriteFile(b.path, rendered, existingMode(b.path, 0o644)); err != nil {
		return err
	}
	return b.reloadAndReconfigure()
}

func (b *networkdBackend) reloadAndReconfigure() error {
	output, err := b.env.Runner.Run("networkctl", "reload")
	if err != nil {
		return commandError("networkctl", []string{"reload"}, output, err)
	}
	output, err = b.env.Runner.Run("networkctl", "reconfigure", b.iface)
	return commandError("networkctl", []string{"reconfigure", b.iface}, output, err)
}

func networkdMatchesInterface(cfg *ini.File, iface string) bool {
	match := cfg.Section("Match")
	for _, value := range sectionShadowValues(match, "Name") {
		for candidate := range strings.FieldsSeq(value) {
			if strings.ContainsAny(candidate, "*?[]!") {
				continue
			}
			if candidate == iface {
				return true
			}
		}
	}
	return false
}

func parseNetworkdDHCP(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "both":
		return true, true
	case "ipv4":
		return true, false
	case "ipv6":
		return false, true
	default:
		return false, false
	}
}

func networkdMethod(dhcp bool, addresses []string) string {
	if dhcp {
		return "auto"
	}
	if len(addresses) > 0 {
		return "manual"
	}
	return "disabled"
}

func ensureSimpleNetworkdLayout(cfg *ini.File) error {
	for _, spec := range []struct {
		name       string
		allowedKey string
	}{
		{name: "Address", allowedKey: "Address"},
		{name: "Route", allowedKey: "Gateway"},
	} {
		section, err := cfg.GetSection(spec.name)
		if err != nil {
			continue
		}
		for _, key := range section.Keys() {
			if key.Name() != spec.allowedKey {
				return unsupportedf("networkd file contains complex [%s] entries", spec.name)
			}
		}
	}
	return nil
}

func updateNetworkdConfig(cfg *ini.File, family int, addressCIDR, gateway string, dns []string, mtu *uint32) error {
	networkSection := cfg.Section("Network")
	addresses := replaceFamilyAddresses(
		append(sectionShadowValues(networkSection, "Address"), sectionShadowValues(cfg.Section("Address"), "Address")...),
		family,
		nonEmptyStringSlice(addressCIDR),
	)
	setShadowValues(networkSection, "Address", addresses)
	cfg.DeleteSection("Address")

	dhcp4, dhcp6 := parseNetworkdDHCP(networkSection.Key("DHCP").String())
	switch family {
	case 4:
		dhcp4 = strings.TrimSpace(addressCIDR) == ""
		gateways := append(sectionShadowValues(networkSection, "Gateway"), sectionShadowValues(cfg.Section("Route"), "Gateway")...)
		if dhcp4 {
			gateways = filterStringSlice(gateways, func(value string) bool { return !isIPv4(value) })
		} else {
			filtered := make([]string, 0, len(gateways)+1)
			for _, value := range gateways {
				if !isIPv4(value) {
					filtered = append(filtered, value)
				}
			}
			filtered = append(filtered, gateway)
			gateways = filtered
		}
		setShadowValues(networkSection, "Gateway", gateways)
		cfg.DeleteSection("Route")
		setShadowValues(networkSection, "DNS", mergeDNSPreservingOtherFamily(sectionShadowValues(networkSection, "DNS"), dns, 4))
	case 6:
		dhcp6 = strings.TrimSpace(addressCIDR) == ""
	}
	switch {
	case dhcp4 && dhcp6:
		networkSection.Key("DHCP").SetValue("yes")
	case dhcp4:
		networkSection.Key("DHCP").SetValue("ipv4")
	case dhcp6:
		networkSection.Key("DHCP").SetValue("ipv6")
	default:
		networkSection.Key("DHCP").SetValue("no")
	}
	if mtu != nil {
		cfg.Section("Link").Key("MTUBytes").SetValue(fmt.Sprintf("%d", *mtu))
	}
	maybeDeleteEmptySection(cfg, "Route")
	maybeDeleteEmptySection(cfg, "Address")
	return nil
}

func firstIPv4(values []string) string {
	for _, value := range values {
		if isIPv4(value) {
			return value
		}
	}
	return ""
}

func nonEmptyStringSlice(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{strings.TrimSpace(value)}
}
