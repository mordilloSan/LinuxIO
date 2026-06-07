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
	apischema.Query[apischema.NoRequest, []apischema.NetworkInterface]("network.get_network_info").Handle(handleGetNetworkInfo),
	apischema.Job[apischema.IPv4ManualRequest, apischema.NoResponse]("network.set_ipv4_manual").Handle(handleSetIPv4Manual),
	apischema.Job[apischema.InterfaceMethodRequest, apischema.NoResponse]("network.set_ipv4").Handle(handleSetIPv4),
	apischema.Job[apischema.InterfaceMethodRequest, apischema.NoResponse]("network.set_ipv6").Handle(handleSetIPv6),
	apischema.Job[apischema.InterfaceMTURequest, apischema.NoResponse]("network.set_mtu").Handle(handleSetMTU),
	apischema.Job[apischema.InterfaceRequest, apischema.NoResponse]("network.enable_connection").Handle(handleEnableConnection),
	apischema.Job[apischema.InterfaceRequest, apischema.NoResponse]("network.disable_connection").Handle(handleDisableConnection),
)

var Routes = api.Routes()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleGetNetworkInfo(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
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
