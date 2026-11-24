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
		return "", fmt.Errorf("list routes: %w", err)
	}

	if gateway := defaultGateway(routes); gateway != "" {
		return gateway, nil
	}
	slog.Debug("no default gateway found", "component", "wireguard", "subsystem", "netlink", "interface", linkIndex)
	return "", fmt.Errorf("default gateway not found")
}

// defaultGateway is the gateway of the first default route. netlink gives a
// default route a Dst of 0.0.0.0/0, as iproute2 does, not a nil one.
func defaultGateway(routes []netlink.Route) string {
	for _, r := range routes {
		if r.Dst != nil {
			if ones, _ := r.Dst.Mask.Size(); ones != 0 {
				continue
			}
		}
		if r.Gw == nil || r.Gw.IsUnspecified() {
			continue
		}
		return r.Gw.String()
	}
	return ""
}
