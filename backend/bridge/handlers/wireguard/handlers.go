package wireguard

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routes = apischema.NewRouteCatalog()

var RouteAddInterface = routes.Job("wireguard.add_interface", apischema.TypeOf[apischema.WireGuardAddInterfaceRequest](), apischema.NoResponse())
var RouteAddPeer = routes.Job("wireguard.add_peer", apischema.TypeOf[apischema.InterfaceNameRequest](), apischema.NoResponse())
var RouteDisableInterface = routes.Job("wireguard.disable_interface", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse())
var RouteDownInterface = routes.Job("wireguard.down_interface", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse())
var RouteEnableInterface = routes.Job("wireguard.enable_interface", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse())
var RouteListInterfaces = routes.Query("wireguard.list_interfaces", apischema.NoRequest(), apischema.TypeOf[[]apischema.WireGuardInterface]())
var RouteListPeers = routes.Query("wireguard.list_peers", apischema.TypeOf[apischema.InterfaceNameRequest](), apischema.TypeOf[[]apischema.Peer]())
var RoutePeerConfigDownload = routes.Query("wireguard.peer_config_download", apischema.TypeOf[apischema.InterfaceNamePeerNameRequest](), apischema.TypeOf[apischema.PeerConfigDownload]())
var RoutePeerQrcode = routes.Query("wireguard.peer_qrcode", apischema.TypeOf[apischema.InterfaceNamePeerNameRequest](), apischema.TypeOf[apischema.QRCodeResponse]())
var RouteRemoveInterface = routes.Job("wireguard.remove_interface", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse())
var RouteRemovePeer = routes.Job("wireguard.remove_peer", apischema.TypeOf[apischema.InterfaceNamePeerNameRequest](), apischema.NoResponse())
var RouteUpInterface = routes.Job("wireguard.up_interface", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse())

var Routes = routes.All()

// RegisterHandlers registers wireguard handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router,
		RouteListInterfaces.Handle(handleListInterfaces),
		RouteAddInterface.Handle(handleAddInterface),
		RouteRemoveInterface.Handle(handleRemoveInterface),
		RouteListPeers.Handle(handleListPeers),
		RouteAddPeer.Handle(handleAddPeer),
		RouteRemovePeer.Handle(handleRemovePeer),
		RoutePeerQrcode.Handle(handlePeerQRCode),
		RoutePeerConfigDownload.Handle(handlePeerConfigDownload),
		RouteUpInterface.Handle(handleUpInterface),
		RouteDownInterface.Handle(handleDownInterface),
		RouteEnableInterface.Handle(handleEnableInterface),
		RouteDisableInterface.Handle(handleDisableInterface),
	)
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
