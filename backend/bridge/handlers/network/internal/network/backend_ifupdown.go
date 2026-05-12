package network

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ifupdownBackend struct {
	baseBackend
}

func (b *ifupdownBackend) Name() string {
	return "ifupdown"
}

func detectIfupdownBackend(env Environment, iface string) (Backend, error) {
	paths := []string{env.IfupdownMain}
	extra, err := globSorted(filepath.Join(env.IfupdownDir, "*"))
	if err != nil {
		return nil, err
	}
	paths = append(paths, extra...)
	var matches []string
	for _, path := range paths {
		if path == "" {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		doc := parseIfupdownDoc(string(raw))
		if doc.hasInterface(iface) {
			matches = append(matches, path)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) > 1 {
		return nil, ambiguousf(iface, "ifupdown", matches)
	}
	return &ifupdownBackend{baseBackend: baseBackend{env: env, iface: iface, path: matches[0]}}, nil
}

func (b *ifupdownBackend) Read() (InterfaceConfig, error) {
	doc, err := b.load()
	if err != nil {
		return InterfaceConfig{}, err
	}
	state, err := doc.interfaceState(b.iface)
	if err != nil {
		return InterfaceConfig{}, err
	}
	result := InterfaceConfig{
		Backend: b.Name(),
	}
	if state.v4 != nil {
		result.IPv4Method = ifupdownMethod(state.v4.Method, state.v4.option("address"))
		if address := state.v4.option("address"); address != "" {
			result.IPv4Addresses = []string{address}
		}
		result.Gateway = state.v4.option("gateway")
		result.DNS = strings.Fields(state.v4.option("dns-nameservers"))
		if mtu := strings.TrimSpace(state.v4.option("mtu")); mtu != "" {
			value, err := parseMTU(mtu)
			if err == nil {
				result.MTU = &value
			}
		}
	} else {
		result.IPv4Method = "unknown"
	}
	if state.v6 != nil {
		result.IPv6Method = ifupdownMethod(state.v6.Method, state.v6.option("address"))
		if address := state.v6.option("address"); address != "" {
			result.IPv6Addresses = []string{address}
		}
	} else {
		result.IPv6Method = "unknown"
	}
	return result, nil
}

func (b *ifupdownBackend) SetIPv4DHCP() error {
	return b.update(func(state *ifupdownInterfaceState) error {
		block := state.ensureV4(b.iface)
		block.Method = "dhcp"
		block.deleteOptions("address", "gateway", "dns-nameservers")
		return nil
	})
}

func (b *ifupdownBackend) SetIPv4Manual(addressCIDR, gateway string, dns []string) error {
	if _, _, err := parseIPv4CIDR(addressCIDR); err != nil {
		return err
	}
	if !isIPv4(gateway) {
		return fmt.Errorf("invalid IPv4 gateway %q", gateway)
	}
	return b.update(func(state *ifupdownInterfaceState) error {
		block := state.ensureV4(b.iface)
		block.Method = "static"
		block.setOption("address", strings.TrimSpace(addressCIDR))
		block.setOption("gateway", strings.TrimSpace(gateway))
		block.setOption("dns-nameservers", strings.Join(dns, " "))
		return nil
	})
}

func (b *ifupdownBackend) SetIPv6DHCP() error {
	return b.update(func(state *ifupdownInterfaceState) error {
		block := state.ensureV6(b.iface)
		block.Method = "auto"
		block.deleteOptions("address")
		return nil
	})
}

func (b *ifupdownBackend) SetIPv6Static(addressCIDR string) error {
	if _, _, err := parseIPv6CIDR(addressCIDR); err != nil {
		return err
	}
	return b.update(func(state *ifupdownInterfaceState) error {
		block := state.ensureV6(b.iface)
		block.Method = "static"
		block.setOption("address", strings.TrimSpace(addressCIDR))
		return nil
	})
}

func (b *ifupdownBackend) SetMTU(mtu uint32) error {
	return b.update(func(state *ifupdownInterfaceState) error {
		block := state.ensureV4(b.iface)
		block.setOption("mtu", fmt.Sprintf("%d", mtu))
		return nil
	})
}

func (b *ifupdownBackend) Enable() error {
	output, err := b.env.Runner.Run("ifup", b.iface)
	return commandError("ifup", []string{b.iface}, output, err)
}

func (b *ifupdownBackend) Disable() error {
	output, err := b.env.Runner.Run("ifdown", b.iface)
	return commandError("ifdown", []string{b.iface}, output, err)
}

func (b *ifupdownBackend) load() (*ifupdownDoc, error) {
	raw, err := os.ReadFile(b.path)
	if err != nil {
		return nil, err
	}
	return parseIfupdownDoc(string(raw)), nil
}

func (b *ifupdownBackend) update(updateFn func(state *ifupdownInterfaceState) error) error {
	doc, err := b.load()
	if err != nil {
		return err
	}
	state, err := doc.interfaceState(b.iface)
	if err != nil {
		return err
	}
	updateErr := updateFn(state)
	if updateErr != nil {
		return updateErr
	}
	writeErr := b.env.WriteFile(b.path, []byte(doc.renderInterfaceState(state)), existingMode(b.path, 0o644))
	if writeErr != nil {
		return writeErr
	}
	if down, downErr := b.env.Runner.Run("ifdown", b.iface); downErr == nil {
		_ = down
	}
	output, err := b.env.Runner.Run("ifup", b.iface)
	return commandError("ifup", []string{b.iface}, output, err)
}

type ifupdownDoc struct {
	items []ifupdownItem
}

type ifupdownItem struct {
	raw   string
	block *ifupdownBlock
}

type ifupdownBlock struct {
	Iface   string
	Family  string
	Method  string
	Options []ifupdownOption
}

type ifupdownOption struct {
	Key   string
	Value string
}

type ifupdownInterfaceState struct {
	doc *ifupdownDoc
	v4  *ifupdownBlock
	v6  *ifupdownBlock
}

func parseIfupdownDoc(data string) *ifupdownDoc {
	lines := strings.Split(data, "\n")
	doc := &ifupdownDoc{items: make([]ifupdownItem, 0, len(lines))}
	for i := 0; i < len(lines); {
		if block, next, ok := parseIfupdownBlock(lines, i); ok {
			doc.items = append(doc.items, ifupdownItem{block: block})
			i = next
			continue
		}
		doc.items = append(doc.items, ifupdownItem{raw: lines[i]})
		i++
	}
	return doc
}

func parseIfupdownBlock(lines []string, start int) (*ifupdownBlock, int, bool) {
	header := strings.Fields(strings.TrimSpace(lines[start]))
	if len(header) < 4 || header[0] != "iface" {
		return nil, start, false
	}
	block := &ifupdownBlock{Iface: header[1], Family: header[2], Method: header[3]}
	next := start + 1
	for next < len(lines) {
		option, consumed := parseIfupdownOption(lines[next])
		if !consumed {
			break
		}
		if option != nil {
			block.Options = append(block.Options, *option)
		}
		next++
	}
	return block, next, true
}

func parseIfupdownOption(line string) (*ifupdownOption, bool) {
	if strings.TrimSpace(line) == "" {
		return nil, true
	}
	if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
		return nil, false
	}
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 2 {
		return nil, true
	}
	return &ifupdownOption{
		Key:   fields[0],
		Value: strings.Join(fields[1:], " "),
	}, true
}

func (d *ifupdownDoc) hasInterface(iface string) bool {
	for _, item := range d.items {
		if item.block != nil && item.block.Iface == iface {
			return true
		}
	}
	return false
}

func (d *ifupdownDoc) interfaceState(iface string) (*ifupdownInterfaceState, error) {
	state := &ifupdownInterfaceState{doc: d}
	for _, item := range d.items {
		if item.block == nil || item.block.Iface != iface {
			continue
		}
		switch item.block.Family {
		case "inet":
			if state.v4 != nil {
				return nil, unsupportedf("multiple inet blocks for interface %s", iface)
			}
			state.v4 = item.block
		case "inet6":
			if state.v6 != nil {
				return nil, unsupportedf("multiple inet6 blocks for interface %s", iface)
			}
			state.v6 = item.block
		}
	}
	return state, nil
}

func (d *ifupdownDoc) renderInterfaceState(state *ifupdownInterfaceState) string {
	var out []string
	inserted := false
	for _, item := range d.items {
		if item.block != nil && item.block.Iface == state.v4Iface() {
			if !inserted {
				inserted = true
				out = append(out, state.render()...)
			}
			continue
		}
		if item.block != nil {
			out = append(out, item.block.render()...)
			continue
		}
		out = append(out, item.raw)
	}
	if !inserted {
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, state.render()...)
	}
	return strings.Join(out, "\n")
}

func (s *ifupdownInterfaceState) v4Iface() string {
	if s.v4 != nil {
		return s.v4.Iface
	}
	if s.v6 != nil {
		return s.v6.Iface
	}
	return ""
}

func (s *ifupdownInterfaceState) ensureV4(iface string) *ifupdownBlock {
	if s.v4 == nil {
		s.v4 = &ifupdownBlock{Iface: iface, Family: "inet", Method: "dhcp"}
	}
	return s.v4
}

func (s *ifupdownInterfaceState) ensureV6(iface string) *ifupdownBlock {
	if s.v6 == nil {
		s.v6 = &ifupdownBlock{Iface: iface, Family: "inet6", Method: "auto"}
	}
	return s.v6
}

func (s *ifupdownInterfaceState) render() []string {
	lines := make([]string, 0, 8)
	if s.v4 != nil {
		lines = append(lines, s.v4.render()...)
	}
	if s.v6 != nil {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, s.v6.render()...)
	}
	return lines
}

func (b *ifupdownBlock) option(key string) string {
	for _, option := range b.Options {
		if option.Key == key {
			return option.Value
		}
	}
	return ""
}

func (b *ifupdownBlock) setOption(key, value string) {
	for i, option := range b.Options {
		if option.Key == key {
			b.Options[i].Value = value
			return
		}
	}
	b.Options = append(b.Options, ifupdownOption{Key: key, Value: value})
}

func (b *ifupdownBlock) deleteOptions(keys ...string) {
	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[key] = struct{}{}
	}
	filtered := b.Options[:0]
	for _, option := range b.Options {
		if _, ok := keySet[option.Key]; ok {
			continue
		}
		filtered = append(filtered, option)
	}
	b.Options = filtered
}

func (b *ifupdownBlock) render() []string {
	lines := []string{fmt.Sprintf("iface %s %s %s", b.Iface, b.Family, b.Method)}
	for _, option := range b.Options {
		lines = append(lines, fmt.Sprintf("\t%s %s", option.Key, option.Value))
	}
	return lines
}

func ifupdownMethod(method, address string) string {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "dhcp", "auto":
		return "auto"
	case "static", "manual":
		if strings.TrimSpace(address) != "" {
			return "manual"
		}
		return "disabled"
	default:
		return "unknown"
	}
}
