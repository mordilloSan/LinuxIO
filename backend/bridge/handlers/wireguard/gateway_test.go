package wireguard

import (
	"net"
	"testing"

	"github.com/vishvananda/netlink"
)

// netlink gives the default route a Dst of 0.0.0.0/0; a nil Dst is what
// older versions gave. Both are the default route.
func TestDefaultGatewayReadsTheDefaultRoute(t *testing.T) {
	subnet := &net.IPNet{IP: net.IPv4(192, 168, 122, 0).To4(), Mask: net.CIDRMask(24, 32)}
	everything := &net.IPNet{IP: net.IPv4zero.To4(), Mask: net.CIDRMask(0, 32)}
	routes := []netlink.Route{
		{Dst: subnet},
		{Dst: everything, Gw: net.IPv4(192, 168, 122, 1)},
	}
	if got := defaultGateway(routes); got != "192.168.122.1" {
		t.Fatalf("defaultGateway = %q, want 192.168.122.1", got)
	}
	if got := defaultGateway([]netlink.Route{{Gw: net.IPv4(10, 0, 0, 1)}}); got != "10.0.0.1" {
		t.Fatalf("defaultGateway with a nil Dst = %q, want 10.0.0.1", got)
	}
	if got := defaultGateway([]netlink.Route{{Dst: subnet, Gw: net.IPv4(192, 168, 122, 1)}, {Dst: everything}}); got != "" {
		t.Fatalf("defaultGateway without one = %q, want none", got)
	}
}
