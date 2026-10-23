package wireguard

import (
	"fmt"
	"log/slog"
	"syscall"

	"github.com/vishvananda/netlink"
)

var (
	getDefaultGatewayIPv4Func      = getDefaultGatewayIPv4
	getGatewayForInterfaceIPv4Func = getGatewayForInterfaceIPv4
)

// getDefaultGatewayIPv4 returns the default IPv4 gateway.
func getDefaultGatewayIPv4() (string, error) {
	return getGatewayFromNetlink(0)
}

// getGatewayForInterfaceIPv4 returns the default IPv4 gateway for a specific interface.
func getGatewayForInterfaceIPv4(iface string) (string, error) {
	link, err := netlink.LinkByName(iface)
	if err != nil {
		slog.Error("failed to lookup interface", "component", "wireguard", "interface", iface, "error", err)
		return "", fmt.Errorf("lookup interface %s: %w", iface, err)
	}
	return getGatewayFromNetlink(link.Attrs().Index)
}

func getGatewayFromNetlink(linkIndex int) (string, error) {
	filter := &netlink.Route{
		Table: syscall.RT_TABLE_MAIN,
	}
	mask := netlink.RT_FILTER_TABLE
	if linkIndex > 0 {
		filter.LinkIndex = linkIndex
		mask |= netlink.RT_FILTER_OIF
	}

	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, filter, mask)
	if err != nil {
		slog.Error("failed to list routes", "component", "wireguard", "subsystem", "netlink", "error", err)
		return "", fmt.Errorf("list routes: %w", err)
	}

	for _, r := range routes {
		if r.Dst != nil {
			continue
		}
		if r.Gw == nil || r.Gw.IsUnspecified() {
			continue
		}
		return r.Gw.String(), nil
	}
	slog.Debug("no default gateway found", "component", "wireguard", "subsystem", "netlink", "interface", linkIndex)
	return "", fmt.Errorf("default gateway not found")
}
