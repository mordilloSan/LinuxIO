package wireguard

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Query[apischema.NoRequest, []apischema.WireGuardInterface]("wireguard.list_interfaces").Handle(handleListInterfaces),
	apischema.Job[apischema.WireGuardAddInterfaceRequest, apischema.NoResponse]("wireguard.add_interface").Handle(handleAddInterface),
	apischema.Job[apischema.NameRequest, apischema.NoResponse]("wireguard.remove_interface").Handle(handleRemoveInterface),
	apischema.Query[apischema.InterfaceNameRequest, []apischema.Peer]("wireguard.list_peers").Handle(handleListPeers),
	apischema.Job[apischema.InterfaceNameRequest, apischema.NoResponse]("wireguard.add_peer").Handle(handleAddPeer),
	apischema.Job[apischema.InterfaceNamePeerNameRequest, apischema.NoResponse]("wireguard.remove_peer").Handle(handleRemovePeer),
	apischema.Query[apischema.InterfaceNamePeerNameRequest, apischema.QRCodeResponse]("wireguard.peer_qrcode").Handle(handlePeerQRCode),
	apischema.Query[apischema.InterfaceNamePeerNameRequest, apischema.PeerConfigDownload]("wireguard.peer_config_download").Handle(handlePeerConfigDownload),
	apischema.Job[apischema.NameRequest, apischema.NoResponse]("wireguard.up_interface").Handle(handleUpInterface),
	apischema.Job[apischema.NameRequest, apischema.NoResponse]("wireguard.down_interface").Handle(handleDownInterface),
	apischema.Job[apischema.NameRequest, apischema.NoResponse]("wireguard.enable_interface").Handle(handleEnableInterface),
	apischema.Job[apischema.NameRequest, apischema.NoResponse]("wireguard.disable_interface").Handle(handleDisableInterface),
)

var Routes = api.Routes()

// RegisterHandlers registers wireguard handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleListInterfaces(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
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
