package wireguard

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Query("wireguard.list_interfaces", apischema.NoRequest(), apischema.TypeOf[[]apischema.WireGuardInterface]()).Handle(handleListInterfaces),
	apischema.Job("wireguard.add_interface", apischema.TypeOf[apischema.WireGuardAddInterfaceRequest](), apischema.NoResponse()).Handle(handleAddInterface),
	apischema.Job("wireguard.remove_interface", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse()).Handle(handleRemoveInterface),
	apischema.Query("wireguard.list_peers", apischema.TypeOf[apischema.InterfaceNameRequest](), apischema.TypeOf[[]apischema.Peer]()).Handle(handleListPeers),
	apischema.Job("wireguard.add_peer", apischema.TypeOf[apischema.InterfaceNameRequest](), apischema.NoResponse()).Handle(handleAddPeer),
	apischema.Job("wireguard.remove_peer", apischema.TypeOf[apischema.InterfaceNamePeerNameRequest](), apischema.NoResponse()).Handle(handleRemovePeer),
	apischema.Query("wireguard.peer_qrcode", apischema.TypeOf[apischema.InterfaceNamePeerNameRequest](), apischema.TypeOf[apischema.QRCodeResponse]()).Handle(handlePeerQRCode),
	apischema.Query("wireguard.peer_config_download", apischema.TypeOf[apischema.InterfaceNamePeerNameRequest](), apischema.TypeOf[apischema.PeerConfigDownload]()).Handle(handlePeerConfigDownload),
	apischema.Job("wireguard.up_interface", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse()).Handle(handleUpInterface),
	apischema.Job("wireguard.down_interface", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse()).Handle(handleDownInterface),
	apischema.Job("wireguard.enable_interface", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse()).Handle(handleEnableInterface),
	apischema.Job("wireguard.disable_interface", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse()).Handle(handleDisableInterface),
)

var Routes = api.Routes()

// RegisterHandlers registers wireguard handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleListInterfaces(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := ListInterfaces(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleAddInterface(ctx context.Context, req apischema.WireGuardAddInterfaceRequest, emit bridgeipc.Events) error {
	result, err := AddInterface(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleRemoveInterface(ctx context.Context, req apischema.NameRequest, emit bridgeipc.Events) error {
	result, err := RemoveInterface(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleListPeers(ctx context.Context, req apischema.InterfaceNameRequest, emit bridgeipc.Events) error {
	result, err := ListPeers(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleAddPeer(ctx context.Context, req apischema.InterfaceNameRequest, emit bridgeipc.Events) error {
	result, err := AddPeer(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleRemovePeer(ctx context.Context, req apischema.InterfaceNamePeerNameRequest, emit bridgeipc.Events) error {
	result, err := RemovePeerByName(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handlePeerQRCode(ctx context.Context, req apischema.InterfaceNamePeerNameRequest, emit bridgeipc.Events) error {
	result, err := PeerQRCode(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handlePeerConfigDownload(ctx context.Context, req apischema.InterfaceNamePeerNameRequest, emit bridgeipc.Events) error {
	result, err := PeerConfigDownload(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleUpInterface(ctx context.Context, req apischema.NameRequest, emit bridgeipc.Events) error {
	result, err := UpInterface(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleDownInterface(ctx context.Context, req apischema.NameRequest, emit bridgeipc.Events) error {
	result, err := DownInterface(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleEnableInterface(ctx context.Context, req apischema.NameRequest, emit bridgeipc.Events) error {
	result, err := EnableInterface(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleDisableInterface(ctx context.Context, req apischema.NameRequest, emit bridgeipc.Events) error {
	result, err := DisableInterface(ctx, req)
	return bridgeipc.EmitResult(emit, result, err)
}
