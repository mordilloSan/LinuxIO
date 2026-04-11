package network

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type ifcfgBackend struct {
	baseBackend
}

func (b *ifcfgBackend) Name() string {
	return "ifcfg"
}

func (b *ifcfgBackend) Read() (InterfaceConfig, error) {
	doc, err := loadKeyValueDoc(b.path)
	if err != nil {
		return InterfaceConfig{}, err
	}
	result := InterfaceConfig{
		Backend: b.Name(),
		DNS:     doc.indexed("DNS"),
		Gateway: strings.TrimSpace(doc.get("GATEWAY")),
	}
	if bootproto := strings.ToLower(strings.TrimSpace(doc.get("BOOTPROTO"))); bootproto == "dhcp" {
		result.IPv4Method = "auto"
	} else if address := strings.TrimSpace(doc.get("IPADDR")); address != "" {
		prefix := strings.TrimSpace(doc.get("PREFIX"))
		if prefix == "" {
			prefix = "24"
		}
		result.IPv4Addresses = []string{fmt.Sprintf("%s/%s", address, prefix)}
		result.IPv4Method = "manual"
	} else if bootproto != "" {
		result.IPv4Method = "disabled"
	} else {
		result.IPv4Method = "unknown"
	}

	switch {
	case strings.EqualFold(doc.get("DHCPV6C"), "yes") || strings.EqualFold(doc.get("IPV6_AUTOCONF"), "yes"):
		result.IPv6Method = "auto"
	case strings.TrimSpace(doc.get("IPV6ADDR")) != "":
		result.IPv6Method = "manual"
		result.IPv6Addresses = []string{strings.TrimSpace(doc.get("IPV6ADDR"))}
	case strings.EqualFold(doc.get("IPV6INIT"), "yes"):
		result.IPv6Method = "disabled"
	default:
		result.IPv6Method = "unknown"
	}
	if mtu := strings.TrimSpace(doc.get("MTU")); mtu != "" {
		value, err := parseMTU(mtu)
		if err == nil {
			result.MTU = &value
		}
	}
	return result, nil
}

func (b *ifcfgBackend) SetIPv4DHCP() error {
	return b.update(func(doc *keyValueDoc) error {
		doc.set("BOOTPROTO", "dhcp")
		for _, key := range []string{"IPADDR", "PREFIX", "NETMASK", "GATEWAY", "PEERDNS"} {
			doc.delete(key)
		}
		doc.deleteIndexed("DNS")
		return nil
	})
}

func (b *ifcfgBackend) SetIPv4Manual(addressCIDR, gateway string, dns []string) error {
	ip, prefix, err := parseIPv4CIDR(addressCIDR)
	if err != nil {
		return err
	}
	if !isIPv4(gateway) {
		return fmt.Errorf("invalid IPv4 gateway %q", gateway)
	}
	return b.update(func(doc *keyValueDoc) error {
		doc.set("BOOTPROTO", "none")
		doc.set("IPADDR", ip)
		doc.set("PREFIX", fmt.Sprintf("%d", prefix))
		doc.set("GATEWAY", strings.TrimSpace(gateway))
		doc.set("PEERDNS", "no")
		doc.setIndexed("DNS", dns)
		return nil
	})
}

func (b *ifcfgBackend) SetIPv6DHCP() error {
	return b.update(func(doc *keyValueDoc) error {
		doc.set("IPV6INIT", "yes")
		doc.set("IPV6_AUTOCONF", "yes")
		doc.set("DHCPV6C", "yes")
		doc.delete("IPV6ADDR")
		return nil
	})
}

func (b *ifcfgBackend) SetIPv6Static(addressCIDR string) error {
	if _, _, err := parseIPv6CIDR(addressCIDR); err != nil {
		return err
	}
	return b.update(func(doc *keyValueDoc) error {
		doc.set("IPV6INIT", "yes")
		doc.set("IPV6_AUTOCONF", "no")
		doc.set("DHCPV6C", "no")
		doc.set("IPV6ADDR", strings.TrimSpace(addressCIDR))
		return nil
	})
}

func (b *ifcfgBackend) SetMTU(mtu uint32) error {
	return b.update(func(doc *keyValueDoc) error {
		doc.set("MTU", fmt.Sprintf("%d", mtu))
		return nil
	})
}

func (b *ifcfgBackend) Enable() error {
	_, err := b.runIfup()
	return err
}

func (b *ifcfgBackend) Disable() error {
	_, err := b.runIfdown()
	return err
}

func (b *ifcfgBackend) update(updateFn func(doc *keyValueDoc) error) error {
	doc, err := loadKeyValueDoc(b.path)
	if err != nil {
		return err
	}
	updateErr := updateFn(doc)
	if updateErr != nil {
		return updateErr
	}
	writeErr := b.env.WriteFile(b.path, doc.render(), existingMode(b.path, 0o644))
	if writeErr != nil {
		return writeErr
	}
	_, _ = b.runIfdown()
	_, err = b.runIfup()
	return err
}

func (b *ifcfgBackend) runIfup() ([]byte, error) {
	output, err := b.env.Runner.Run("ifup", b.iface)
	if err == nil {
		return output, nil
	}
	fallback, fallbackErr := b.env.Runner.Run("systemctl", "restart", "network")
	if fallbackErr == nil {
		return fallback, nil
	}
	return output, commandError("ifup", []string{b.iface}, output, err)
}

func (b *ifcfgBackend) runIfdown() ([]byte, error) {
	output, err := b.env.Runner.Run("ifdown", b.iface)
	if err == nil {
		return output, nil
	}
	return output, commandError("ifdown", []string{b.iface}, output, err)
}

type keyValueDoc struct {
	lines []keyValueLine
}

type keyValueLine struct {
	raw    string
	key    string
	value  string
	isPair bool
}

func loadKeyValueDoc(path string) (*keyValueDoc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseKeyValueDoc(raw), nil
}

func parseKeyValueDoc(data []byte) *keyValueDoc {
	lines := bytes.Split(data, []byte("\n"))
	doc := &keyValueDoc{lines: make([]keyValueLine, 0, len(lines))}
	for _, line := range lines {
		text := string(line)
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || !strings.Contains(text, "=") {
			doc.lines = append(doc.lines, keyValueLine{raw: text})
			continue
		}
		parts := strings.SplitN(text, "=", 2)
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		doc.lines = append(doc.lines, keyValueLine{
			key:    key,
			value:  value,
			isPair: true,
		})
	}
	return doc
}

func (d *keyValueDoc) get(key string) string {
	for _, line := range d.lines {
		if line.isPair && line.key == key {
			return line.value
		}
	}
	return ""
}

func (d *keyValueDoc) set(key, value string) {
	for i, line := range d.lines {
		if line.isPair && line.key == key {
			d.lines[i].value = value
			return
		}
	}
	d.lines = append(d.lines, keyValueLine{key: key, value: value, isPair: true})
}

func (d *keyValueDoc) delete(key string) {
	filtered := d.lines[:0]
	for _, line := range d.lines {
		if line.isPair && line.key == key {
			continue
		}
		filtered = append(filtered, line)
	}
	d.lines = filtered
}

func (d *keyValueDoc) deleteIndexed(prefix string) {
	filtered := d.lines[:0]
	for _, line := range d.lines {
		if line.isPair && strings.HasPrefix(line.key, prefix) {
			suffix := strings.TrimPrefix(line.key, prefix)
			if suffix == "" {
				continue
			}
			if _, err := strconv.Atoi(suffix); err == nil {
				continue
			}
		}
		filtered = append(filtered, line)
	}
	d.lines = filtered
}

func (d *keyValueDoc) indexed(prefix string) []string {
	values := make([]string, 0)
	for _, line := range d.lines {
		if line.isPair && strings.HasPrefix(line.key, prefix) {
			values = append(values, strings.TrimSpace(line.value))
		}
	}
	return values
}

func (d *keyValueDoc) setIndexed(prefix string, values []string) {
	d.deleteIndexed(prefix)
	for i, value := range values {
		d.lines = append(d.lines, keyValueLine{
			key:    fmt.Sprintf("%s%d", prefix, i+1),
			value:  strings.TrimSpace(value),
			isPair: true,
		})
	}
}

func (d *keyValueDoc) render() []byte {
	var builder strings.Builder
	for i, line := range d.lines {
		if i > 0 {
			builder.WriteByte('\n')
		}
		if line.isPair {
			builder.WriteString(line.key)
			builder.WriteByte('=')
			builder.WriteString(line.value)
			continue
		}
		builder.WriteString(line.raw)
	}
	return []byte(builder.String())
}
