package network

import (
	"context"
	"fmt"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func routeBindings(rt runtime.Runtime) apischema.BindingSet {
	h := networkHandlers{rt: rt, handoff: validatingBridgeHandoffService{inner: newBridgeHandoffAdapter(rt)}}
	return apischema.Bindings(
		apischema.Call[apischema.NoRequest, []apischema.NetworkInterface]("network.get_network_info").Handle(h.handleGetNetworkInfo),
		apischema.Call[apischema.NoRequest, []apischema.InterfaceStats]("network.get_interface_stats").Handle(h.handleGetInterfaceStats),
		apischema.Call[apischema.NoRequest, apischema.NetworkBridgeOptions]("network.get_bridge_options", apischema.RetrySafe(), apischema.Privileged()).Handle(h.handleGetBridgeOptions),
		apischema.Call[apischema.NetworkBridgeCreateRequest, apischema.NetworkBridgeCreateResult]("network.create_bridge", apischema.Privileged()).Handle(h.handleCreateBridge),
		apischema.Call[apischema.NetworkBridgeHandoffRequest, apischema.NetworkBridgeHandoffStatus]("network.start_bridge_handoff", apischema.Privileged()).Handle(h.handleStartBridgeHandoff),
		apischema.Call[apischema.NetworkBridgeHandoffOperationRequest, apischema.NetworkBridgeHandoffStatus]("network.get_bridge_handoff", apischema.RetrySafe(), apischema.Privileged()).Handle(h.handleGetBridgeHandoff),
		apischema.Call[apischema.NetworkBridgeHandoffOperationRequest, apischema.NetworkBridgeHandoffStatus]("network.confirm_bridge_handoff", apischema.RetrySafe(), apischema.Privileged()).Handle(h.handleConfirmBridgeHandoff),
		apischema.Call[apischema.NetworkBridgeHandoffOperationRequest, apischema.NetworkBridgeHandoffStatus]("network.revert_bridge_handoff", apischema.RetrySafe(), apischema.Privileged()).Handle(h.handleRevertBridgeHandoff),
		// NetworkManager owns an accepted configuration change. Applying it can
		// sever this bridge, so transport loss is an expected ambiguous outcome and
		// callers must not retry the mutation automatically.
		apischema.Call[apischema.IPv4ManualRequest, apischema.NoResponse]("network.set_ipv4_manual").HandleVoid(h.handleSetIPv4Manual),
		apischema.Call[apischema.InterfaceMethodRequest, apischema.NoResponse]("network.set_ipv4").HandleVoid(h.handleSetIPv4),
		apischema.Call[apischema.InterfaceMethodRequest, apischema.NoResponse]("network.set_ipv6").HandleVoid(h.handleSetIPv6),
		apischema.Call[apischema.InterfaceMTURequest, apischema.NoResponse]("network.set_mtu").HandleVoid(h.handleSetMTU),
		apischema.Call[apischema.InterfaceRequest, apischema.NoResponse]("network.enable_connection").HandleVoid(h.handleEnableConnection),
		apischema.Call[apischema.InterfaceRequest, apischema.NoResponse]("network.disable_connection").HandleVoid(h.handleDisableConnection),
	)
}

var Routes = routeBindings(runtime.Runtime{}).Routes()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	routeBindings(rt).Register(router)
}

func (h networkHandlers) handleGetNetworkInfo(ctx context.Context, _ apischema.NoRequest) ([]apischema.NetworkInterface, error) {
	return GetNetworkInfo(ctx)
}

func (h networkHandlers) handleGetInterfaceStats(ctx context.Context, _ apischema.NoRequest) ([]apischema.InterfaceStats, error) {
	return FetchInterfaceStats(ctx)
}

func (h networkHandlers) handleGetBridgeOptions(ctx context.Context, _ apischema.NoRequest) (apischema.NetworkBridgeOptions, error) {
	return GetBridgeOptions(ctx)
}

func (h networkHandlers) handleCreateBridge(ctx context.Context, req apischema.NetworkBridgeCreateRequest) (apischema.NetworkBridgeCreateResult, error) {
	return CreateBridge(ctx, req)
}

func (h networkHandlers) handleStartBridgeHandoff(ctx context.Context, req apischema.NetworkBridgeHandoffRequest) (apischema.NetworkBridgeHandoffStatus, error) {
	return h.handoff.Start(ctx, h.handleOwnerUID(), req)
}

func (h networkHandlers) handleGetBridgeHandoff(ctx context.Context, req apischema.NetworkBridgeHandoffOperationRequest) (apischema.NetworkBridgeHandoffStatus, error) {
	return h.handoff.Status(ctx, h.handleOwnerUID(), req.OperationID)
}

func (h networkHandlers) handleConfirmBridgeHandoff(ctx context.Context, req apischema.NetworkBridgeHandoffOperationRequest) (apischema.NetworkBridgeHandoffStatus, error) {
	return h.handoff.Confirm(ctx, h.handleOwnerUID(), req.OperationID)
}

func (h networkHandlers) handleRevertBridgeHandoff(ctx context.Context, req apischema.NetworkBridgeHandoffOperationRequest) (apischema.NetworkBridgeHandoffStatus, error) {
	return h.handoff.Revert(ctx, h.handleOwnerUID(), req.OperationID)
}

func (h networkHandlers) handleOwnerUID() uint32 {
	if h.rt.Session == nil {
		return 0
	}
	return h.rt.Session.User.UID
}

func (h networkHandlers) handleSetIPv4Manual(ctx context.Context, req apischema.IPv4ManualRequest) error {
	return SetIPv4Manual(ctx, req.Iface, req.Address, req.Gateway, []string{req.DNS})
}

func (h networkHandlers) handleSetIPv4(ctx context.Context, req apischema.InterfaceMethodRequest) error {
	method := strings.ToLower(req.Method)
	if method != "dhcp" && method != "auto" {
		return fmt.Errorf("SetIPv4 method must be 'dhcp' or 'static'")
	}
	return SetIPv4DHCP(ctx, req.Iface)
}

func (h networkHandlers) handleSetIPv6(ctx context.Context, req apischema.InterfaceMethodRequest) error {
	method := strings.ToLower(req.Method)
	switch method {
	case "dhcp", "auto":
		return SetIPv6DHCP(ctx, req.Iface)
	default:
		return fmt.Errorf("SetIPv6 method must be 'dhcp' or 'auto'")
	}
}

func (h networkHandlers) handleSetMTU(ctx context.Context, req apischema.InterfaceMTURequest) error {
	return SetMTU(ctx, req.Iface, req.MTU)
}

func (h networkHandlers) handleEnableConnection(ctx context.Context, req apischema.InterfaceRequest) error {
	return EnableConnection(ctx, req.Iface)
}

func (h networkHandlers) handleDisableConnection(ctx context.Context, req apischema.InterfaceRequest) error {
	return DisableConnection(ctx, req.Iface)
}
