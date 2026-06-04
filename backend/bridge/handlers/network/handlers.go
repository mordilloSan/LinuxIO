package network

import (
	"context"
	"fmt"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routes = apischema.NewRouteCatalog()

var RouteDisableConnection = routes.Job("network.disable_connection", apischema.TypeOf[apischema.InterfaceRequest](), apischema.NoResponse())
var RouteEnableConnection = routes.Job("network.enable_connection", apischema.TypeOf[apischema.InterfaceRequest](), apischema.NoResponse())
var RouteGetNetworkInfo = routes.Query("network.get_network_info", apischema.NoRequest(), apischema.TypeOf[[]apischema.NetworkInterface]())
var RouteSetIPv4 = routes.Job("network.set_ipv4", apischema.TypeOf[apischema.InterfaceMethodRequest](), apischema.NoResponse())
var RouteSetIPv4Manual = routes.Job("network.set_ipv4_manual", apischema.TypeOf[apischema.IPv4ManualRequest](), apischema.NoResponse())
var RouteSetIPv6 = routes.Job("network.set_ipv6", apischema.TypeOf[apischema.InterfaceMethodRequest](), apischema.NoResponse())
var RouteSetMTU = routes.Job("network.set_mtu", apischema.TypeOf[apischema.InterfaceMTURequest](), apischema.NoResponse())

var Routes = routes.All()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router,
		RouteGetNetworkInfo.Handle(handleGetNetworkInfo),
		RouteSetIPv4Manual.Handle(handleSetIPv4Manual),
		RouteSetIPv4.Handle(handleSetIPv4),
		RouteSetIPv6.Handle(handleSetIPv6),
		RouteSetMTU.Handle(handleSetMTU),
		RouteEnableConnection.Handle(handleEnableConnection),
		RouteDisableConnection.Handle(handleDisableConnection),
	)
}

func handleGetNetworkInfo(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := GetNetworkInfo(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleSetIPv4Manual(ctx context.Context, req apischema.IPv4ManualRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, SetIPv4Manual(ctx, req.Iface, req.Address, req.Gateway, []string{req.DNS}))
}

func handleSetIPv4(ctx context.Context, req apischema.InterfaceMethodRequest, emit bridgeipc.Events) error {
	method := strings.ToLower(req.Method)
	if method != "dhcp" && method != "auto" {
		return fmt.Errorf("SetIPv4 method must be 'dhcp' or 'static'")
	}
	return bridgeipc.EmitResult(emit, nil, SetIPv4DHCP(ctx, req.Iface))
}

func handleSetIPv6(ctx context.Context, req apischema.InterfaceMethodRequest, emit bridgeipc.Events) error {
	method := strings.ToLower(req.Method)
	switch method {
	case "dhcp", "auto":
		return bridgeipc.EmitResult(emit, nil, SetIPv6DHCP(ctx, req.Iface))
	default:
		return fmt.Errorf("SetIPv6 method must be 'dhcp' or 'auto'")
	}
}

func handleSetMTU(ctx context.Context, req apischema.InterfaceMTURequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, SetMTU(ctx, req.Iface, req.MTU))
}

func handleEnableConnection(ctx context.Context, req apischema.InterfaceRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, EnableConnection(ctx, req.Iface))
}

func handleDisableConnection(ctx context.Context, req apischema.InterfaceRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, DisableConnection(ctx, req.Iface))
}
