package network

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	ini "gopkg.in/ini.v1"
)

type nmConnectionBackend struct {
	baseBackend
	connectionID string
	uuid         string
}

func (b *nmConnectionBackend) Name() string {
	return "nmconnection"
}

func detectNMConnectionBackend(env Environment, iface string) (Backend, error) {
	paths, err := globSorted(filepath.Join(env.NMConnectionDir, "*.nmconnection"))
	if err != nil {
		return nil, err
	}
	var matches []string
	var ids []string
	var uuids []string
	for _, path := range paths {
		cfg, err := readINIFile(path)
		if err != nil {
			return nil, fmt.Errorf("parse nmconnection %s: %w", path, err)
		}
		if !nmConnectionMatchesInterface(cfg, iface) {
			continue
		}
		connection := cfg.Section("connection")
		matches = append(matches, path)
		ids = append(ids, connection.Key("id").String())
		uuids = append(uuids, connection.Key("uuid").String())
	}
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) > 1 {
		return nil, ambiguousf(iface, "nmconnection", matches)
	}
	return &nmConnectionBackend{
		baseBackend:  baseBackend{env: env, iface: iface, path: matches[0]},
		connectionID: ids[0],
		uuid:         uuids[0],
	}, nil
}

func (b *nmConnectionBackend) Read() (InterfaceConfig, error) {
	cfg, err := readINIFile(b.path)
	if err != nil {
		return InterfaceConfig{}, err
	}
	ipv4 := cfg.Section("ipv4")
	ipv6 := cfg.Section("ipv6")
	result := InterfaceConfig{
		Backend:       b.Name(),
		IPv4Method:    nmMethod(ipv4.Key("method").String()),
		IPv4Addresses: nmAddresses(ipv4, 4),
		IPv6Method:    nmMethod(ipv6.Key("method").String()),
		IPv6Addresses: nmAddresses(ipv6, 6),
		DNS:           append(nmDNS(ipv4), nmDNS(ipv6)...),
		Gateway:       strings.TrimSpace(ipv4.Key("gateway").String()),
	}
	if mtu := nmMTU(cfg); mtu != nil {
		result.MTU = mtu
	}
	return result, nil
}

func (b *nmConnectionBackend) SetIPv4DHCP() error {
	return b.update(func(cfg *ini.File) error {
		section := cfg.Section("ipv4")
		section.Key("method").SetValue("auto")
		deletePrefixedKeys(section, "address")
		deletePrefixedKeys(section, "route")
		deletePrefixedKeys(section, "routing-rule")
		section.DeleteKey("gateway")
		mergedDNS := mergeDNSPreservingOtherFamily(append(nmDNS(section), nmDNS(cfg.Section("ipv6"))...), nil, 4)
		if dns := filterStringSlice(mergedDNS, isIPv4); len(dns) > 0 {
			section.Key("dns").SetValue(nmDNSValue(dns))
		} else {
			section.DeleteKey("dns")
		}
		for _, key := range []string{"ignore-auto-dns", "ignore-auto-routes", "never-default", "may-fail"} {
			section.DeleteKey(key)
		}
		return nil
	})
}

func (b *nmConnectionBackend) SetIPv4Manual(addressCIDR, gateway string, dns []string) error {
	if _, _, err := parseIPv4CIDR(addressCIDR); err != nil {
		return err
	}
	if !isIPv4(gateway) {
		return fmt.Errorf("invalid IPv4 gateway %q", gateway)
	}
	return b.update(func(cfg *ini.File) error {
		section := cfg.Section("ipv4")
		section.Key("method").SetValue("manual")
		deletePrefixedKeys(section, "address")
		deletePrefixedKeys(section, "route")
		deletePrefixedKeys(section, "routing-rule")
		section.Key("address1").SetValue(strings.TrimSpace(addressCIDR))
		section.Key("gateway").SetValue(strings.TrimSpace(gateway))
		merged := mergeDNSPreservingOtherFamily(append(nmDNS(section), nmDNS(cfg.Section("ipv6"))...), dns, 4)
		ipv4DNS := filterStringSlice(merged, isIPv4)
		if len(ipv4DNS) > 0 {
			section.Key("dns").SetValue(nmDNSValue(ipv4DNS))
		} else {
			section.DeleteKey("dns")
		}
		section.Key("ignore-auto-dns").SetValue("true")
		section.Key("ignore-auto-routes").SetValue("true")
		section.Key("never-default").SetValue("false")
		section.Key("may-fail").SetValue("false")
		return nil
	})
}

func (b *nmConnectionBackend) SetIPv6DHCP() error {
	return b.update(func(cfg *ini.File) error {
		section := cfg.Section("ipv6")
		section.Key("method").SetValue("auto")
		deletePrefixedKeys(section, "address")
		deletePrefixedKeys(section, "route")
		deletePrefixedKeys(section, "routing-rule")
		for _, key := range []string{"gateway", "dns", "ignore-auto-dns", "ignore-auto-routes", "never-default", "may-fail"} {
			section.DeleteKey(key)
		}
		return nil
	})
}

func (b *nmConnectionBackend) SetIPv6Static(addressCIDR string) error {
	if _, _, err := parseIPv6CIDR(addressCIDR); err != nil {
		return err
	}
	return b.update(func(cfg *ini.File) error {
		section := cfg.Section("ipv6")
		section.Key("method").SetValue("manual")
		deletePrefixedKeys(section, "address")
		deletePrefixedKeys(section, "route")
		deletePrefixedKeys(section, "routing-rule")
		section.Key("address1").SetValue(strings.TrimSpace(addressCIDR))
		return nil
	})
}

func (b *nmConnectionBackend) SetMTU(mtu uint32) error {
	return b.update(func(cfg *ini.File) error {
		mtuSection := nmMTUSection(cfg)
		mtuSection.Key("mtu").SetValue(strconv.FormatUint(uint64(mtu), 10))
		return nil
	})
}

func (b *nmConnectionBackend) Enable() error {
	if err := b.loadConnection(); err != nil {
		return err
	}
	targetArgs := b.connectionTargetArgs()
	output, err := b.env.Runner.Run("nmcli", append([]string{"connection", "up"}, targetArgs...)...)
	return commandError("nmcli", append([]string{"connection", "up"}, targetArgs...), output, err)
}

func (b *nmConnectionBackend) Disable() error {
	output, err := b.env.Runner.Run("nmcli", "device", "disconnect", b.iface)
	if err == nil {
		return nil
	}
	targetArgs := b.connectionTargetArgs()
	_, fallbackErr := b.env.Runner.Run("nmcli", append([]string{"connection", "down"}, targetArgs...)...)
	if fallbackErr == nil {
		return nil
	}
	return commandError("nmcli", []string{"device", "disconnect", b.iface}, output, err)
}

func (b *nmConnectionBackend) update(updateFn func(cfg *ini.File) error) error {
	cfg, err := readINIFile(b.path)
	if err != nil {
		return err
	}
	updateErr := updateFn(cfg)
	if updateErr != nil {
		return updateErr
	}
	rendered, err := renderINI(cfg)
	if err != nil {
		return err
	}
	if err := b.env.WriteFile(b.path, rendered, existingMode(b.path, 0o600)); err != nil {
		return err
	}
	return b.reloadAndReapply()
}

func (b *nmConnectionBackend) reloadAndReapply() error {
	if err := b.loadConnection(); err != nil {
		return err
	}
	output, err := b.env.Runner.Run("nmcli", "device", "reapply", b.iface)
	if err == nil {
		return nil
	}
	targetArgs := b.connectionTargetArgs()
	_, fallbackErr := b.env.Runner.Run("nmcli", append([]string{"connection", "up"}, targetArgs...)...)
	if fallbackErr == nil {
		return nil
	}
	return commandError("nmcli", []string{"device", "reapply", b.iface}, output, err)
}

func (b *nmConnectionBackend) loadConnection() error {
	args := []string{"connection", "load", b.path}
	output, err := b.env.Runner.Run("nmcli", args...)
	return commandError("nmcli", args, output, err)
}

func (b *nmConnectionBackend) connectionTargetArgs() []string {
	if strings.TrimSpace(b.uuid) != "" {
		return []string{"uuid", b.uuid}
	}
	return []string{"id", b.connectionID}
}

func readINIFile(path string) (*ini.File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return loadINI(raw)
}

func nmConnectionMatchesInterface(cfg *ini.File, iface string) bool {
	connection := cfg.Section("connection")
	if strings.TrimSpace(connection.Key("interface-name").String()) == iface {
		return true
	}
	matchDevice := connection.Key("match-device").String()
	for part := range strings.SplitSeq(matchDevice, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "interface-name:") && strings.TrimPrefix(part, "interface-name:") == iface {
			return true
		}
	}
	return false
}

func nmMethod(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto":
		return "auto"
	case "manual":
		return "manual"
	case "disabled", "ignore":
		return "disabled"
	default:
		return "unknown"
	}
}

func nmAddresses(section *ini.Section, family int) []string {
	var addresses []string
	for _, key := range section.Keys() {
		if !strings.HasPrefix(key.Name(), "address") {
			continue
		}
		value := key.String()
		if idx := strings.IndexAny(value, ",;"); idx >= 0 {
			value = value[:idx]
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if family == 4 {
			if ip, _, err := parseCIDR(value); err == nil && ip.To4() != nil {
				addresses = append(addresses, value)
			}
			continue
		}
		if ip, _, err := parseCIDR(value); err == nil && ip.To16() != nil && ip.To4() == nil {
			addresses = append(addresses, value)
		}
	}
	return addresses
}

func nmDNS(section *ini.Section) []string {
	key, err := section.GetKey("dns")
	if err != nil {
		return nil
	}
	value := key.String()
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ';' || r == ','
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func nmDNSValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, ";") + ";"
}

func nmMTU(cfg *ini.File) *uint32 {
	for _, name := range []string{"ethernet", "802-3-ethernet", "wifi", "802-11-wireless"} {
		section, err := cfg.GetSection(name)
		if err != nil {
			continue
		}
		if value := strings.TrimSpace(section.Key("mtu").String()); value != "" {
			mtu, err := parseMTU(value)
			if err == nil {
				return &mtu
			}
		}
	}
	return nil
}

func nmMTUSection(cfg *ini.File) *ini.Section {
	connectionType := cfg.Section("connection").Key("type").String()
	switch connectionType {
	case "802-3-ethernet":
		return cfg.Section("ethernet")
	case "802-11-wireless":
		return cfg.Section("wifi")
	default:
		for _, name := range []string{"ethernet", "802-3-ethernet", "wifi", "802-11-wireless"} {
			section, err := cfg.GetSection(name)
			if err == nil {
				return section
			}
		}
		return cfg.Section("ethernet")
	}
}

func filterStringSlice(values []string, keep func(string) bool) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if keep(value) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}
