package network

import (
	"context"
	"fmt"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Call[apischema.NoRequest, []apischema.NetworkInterface]("network.get_network_info").Handle(handleGetNetworkInfo),
	apischema.Call[apischema.NoRequest, []apischema.InterfaceStats]("network.get_interface_stats").Handle(handleGetInterfaceStats),
	// NetworkManager owns an accepted configuration change. Applying it can
	// sever this bridge, so transport loss is an expected ambiguous outcome and
	// callers must not retry the mutation automatically.
	apischema.Call[apischema.IPv4ManualRequest, apischema.NoResponse]("network.set_ipv4_manual").HandleVoid(handleSetIPv4Manual),
	apischema.Call[apischema.InterfaceMethodRequest, apischema.NoResponse]("network.set_ipv4").HandleVoid(handleSetIPv4),
	apischema.Call[apischema.InterfaceMethodRequest, apischema.NoResponse]("network.set_ipv6").HandleVoid(handleSetIPv6),
	apischema.Call[apischema.InterfaceMTURequest, apischema.NoResponse]("network.set_mtu").HandleVoid(handleSetMTU),
	apischema.Call[apischema.InterfaceRequest, apischema.NoResponse]("network.enable_connection").HandleVoid(handleEnableConnection),
	apischema.Call[apischema.InterfaceRequest, apischema.NoResponse]("network.disable_connection").HandleVoid(handleDisableConnection),
)

var Routes = api.Routes()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleGetNetworkInfo(ctx context.Context, _ apischema.NoRequest) ([]apischema.NetworkInterface, error) {
	return GetNetworkInfo(ctx)
}

func handleGetInterfaceStats(ctx context.Context, _ apischema.NoRequest) ([]apischema.InterfaceStats, error) {
	return FetchInterfaceStats(ctx)
}

func handleSetIPv4Manual(ctx context.Context, req apischema.IPv4ManualRequest) error {
	return SetIPv4Manual(ctx, req.Iface, req.Address, req.Gateway, []string{req.DNS})
}

func handleSetIPv4(ctx context.Context, req apischema.InterfaceMethodRequest) error {
	method := strings.ToLower(req.Method)
	if method != "dhcp" && method != "auto" {
		return fmt.Errorf("SetIPv4 method must be 'dhcp' or 'static'")
	}
	return SetIPv4DHCP(ctx, req.Iface)
}

func handleSetIPv6(ctx context.Context, req apischema.InterfaceMethodRequest) error {
	method := strings.ToLower(req.Method)
	switch method {
	case "dhcp", "auto":
		return SetIPv6DHCP(ctx, req.Iface)
	default:
		return fmt.Errorf("SetIPv6 method must be 'dhcp' or 'auto'")
	}
}

func handleSetMTU(ctx context.Context, req apischema.InterfaceMTURequest) error {
	return SetMTU(ctx, req.Iface, req.MTU)
}

func handleEnableConnection(ctx context.Context, req apischema.InterfaceRequest) error {
	return EnableConnection(ctx, req.Iface)
}

func handleDisableConnection(ctx context.Context, req apischema.InterfaceRequest) error {
	return DisableConnection(ctx, req.Iface)
}
