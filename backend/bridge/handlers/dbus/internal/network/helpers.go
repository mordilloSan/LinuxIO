package network

import (
	"fmt"
	stdnet "net"
	"strconv"
	"strings"

	"github.com/vishvananda/netlink"
)

type baseBackend struct {
	env   Environment
	iface string
	path  string
}

func parseCIDR(addressCIDR string) (stdnet.IP, int, error) {
	ip, network, err := stdnet.ParseCIDR(strings.TrimSpace(addressCIDR))
	if err != nil {
		return nil, 0, err
	}
	prefix, bits := network.Mask.Size()
	if prefix < 0 || bits == 0 {
		return nil, 0, fmt.Errorf("invalid prefix in %q", addressCIDR)
	}
	return ip, prefix, nil
}

func parseIPv4CIDR(addressCIDR string) (string, int, error) {
	ip, prefix, err := parseCIDR(addressCIDR)
	if err != nil {
		return "", 0, err
	}
	if ip.To4() == nil {
		return "", 0, fmt.Errorf("not an IPv4 CIDR: %s", addressCIDR)
	}
	return ip.String(), prefix, nil
}

func parseIPv6CIDR(addressCIDR string) (string, int, error) {
	ip, prefix, err := parseCIDR(addressCIDR)
	if err != nil {
		return "", 0, err
	}
	if ip.To16() == nil || ip.To4() != nil {
		return "", 0, fmt.Errorf("not an IPv6 CIDR: %s", addressCIDR)
	}
	return ip.String(), prefix, nil
}

func isIPv4(value string) bool {
	ip := stdnet.ParseIP(strings.TrimSpace(value))
	return ip != nil && ip.To4() != nil
}

func isIPv6(value string) bool {
	ip := stdnet.ParseIP(strings.TrimSpace(value))
	return ip != nil && ip.To4() == nil
}

func filterAddressesByFamily(addresses []string, family int) []string {
	filtered := make([]string, 0, len(addresses))
	for _, address := range addresses {
		ip, _, err := stdnet.ParseCIDR(address)
		if err != nil || ip == nil {
			continue
		}
		switch family {
		case 4:
			if ip.To4() != nil {
				filtered = append(filtered, address)
			}
		case 6:
			if ip.To16() != nil && ip.To4() == nil {
				filtered = append(filtered, address)
			}
		}
	}
	return filtered
}

func replaceFamilyAddresses(existing []string, family int, replacement []string) []string {
	updated := make([]string, 0, len(existing)+len(replacement))
	for _, address := range existing {
		ip, _, err := stdnet.ParseCIDR(address)
		if err != nil || ip == nil {
			updated = append(updated, address)
			continue
		}
		if family == 4 && ip.To4() != nil {
			continue
		}
		if family == 6 && ip.To16() != nil && ip.To4() == nil {
			continue
		}
		updated = append(updated, address)
	}
	return append(updated, replacement...)
}

func mergeDNSPreservingOtherFamily(existing, replacement []string, family int) []string {
	merged := make([]string, 0, len(existing)+len(replacement))
	for _, entry := range existing {
		switch family {
		case 4:
			if isIPv4(entry) {
				continue
			}
		case 6:
			if isIPv6(entry) {
				continue
			}
		}
		merged = append(merged, entry)
	}
	seen := make(map[string]struct{}, len(merged)+len(replacement))
	for _, entry := range merged {
		seen[entry] = struct{}{}
	}
	for _, entry := range replacement {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		merged = append(merged, entry)
	}
	return merged
}

func parseMTU(mtu string) (uint32, error) {
	value, err := strconv.ParseUint(strings.TrimSpace(mtu), 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(value), nil
}

func setLinkUp(iface string) error {
	link, err := netlink.LinkByName(iface)
	if err != nil {
		return err
	}
	return netlink.LinkSetUp(link)
}

func setLinkDown(iface string) error {
	link, err := netlink.LinkByName(iface)
	if err != nil {
		return err
	}
	return netlink.LinkSetDown(link)
}

func commandError(name string, args []string, output []byte, err error) error {
	if err == nil {
		return nil
	}
	text := strings.TrimSpace(string(output))
	if text == "" {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, text)
}
