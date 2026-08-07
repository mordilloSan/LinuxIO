package wireguard

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// Every NoResponse route here is bound with HandleVoid. Their domain functions
// return `(any, error)` and were emitting real payloads — status maps, command
// output, `"removed"`, and in add_interface's case the generated *private key* —
// onto routes whose generated TypeScript is `void`. Every consumer discards the
// result (`success: (_result: void, variables) => ...`), so the payloads were
// unreachable by design and, for job routes, sat in the job snapshot for
// DefaultTerminalJobTTL where jobs.get/jobs.list could read them back.
var api = apischema.Bindings(
	apischema.Query[apischema.NoRequest, []apischema.WireGuardInterface]("wireguard.list_interfaces").Handle(handleListInterfaces),
	apischema.Query[apischema.WireGuardAddInterfaceRequest, apischema.NoResponse]("wireguard.add_interface").HandleVoid(handleAddInterface),
	apischema.Query[apischema.NameRequest, apischema.NoResponse]("wireguard.remove_interface").HandleVoid(handleRemoveInterface),
	apischema.Query[apischema.InterfaceNameRequest, []apischema.Peer]("wireguard.list_peers").Handle(handleListPeers),
	apischema.Query[apischema.InterfaceNameRequest, apischema.NoResponse]("wireguard.add_peer").HandleVoid(handleAddPeer),
	apischema.Query[apischema.InterfaceNamePeerNameRequest, apischema.NoResponse]("wireguard.remove_peer").HandleVoid(handleRemovePeer),
	apischema.Query[apischema.InterfaceNamePeerNameRequest, apischema.QRCodeResponse]("wireguard.peer_qrcode").Handle(handlePeerQRCode),
	apischema.Query[apischema.InterfaceNamePeerNameRequest, apischema.PeerConfigDownload]("wireguard.peer_config_download").Handle(handlePeerConfigDownload),
	apischema.Query[apischema.NameRequest, apischema.NoResponse]("wireguard.up_interface").HandleVoid(handleUpInterface),
	apischema.Query[apischema.NameRequest, apischema.NoResponse]("wireguard.down_interface").HandleVoid(handleDownInterface),
	apischema.Query[apischema.NameRequest, apischema.NoResponse]("wireguard.enable_interface").HandleVoid(handleEnableInterface),
	apischema.Query[apischema.NameRequest, apischema.NoResponse]("wireguard.disable_interface").HandleVoid(handleDisableInterface),
)

var Routes = api.Routes()

// RegisterHandlers registers wireguard handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleListInterfaces(ctx context.Context, _ apischema.NoRequest) ([]apischema.WireGuardInterface, error) {
	result, err := ListInterfaces(ctx)
	return wireGuardInterfacesToAPI(result), err
}

// AddInterface's result carries the generated private key; discarding it is the
// point. See the binding comment above.
func handleAddInterface(ctx context.Context, req apischema.WireGuardAddInterfaceRequest) error {
	_, err := AddInterface(ctx, req)
	return err
}

func handleRemoveInterface(ctx context.Context, req apischema.NameRequest) error {
	_, err := RemoveInterface(ctx, req)
	return err
}

func handleListPeers(ctx context.Context, req apischema.InterfaceNameRequest) ([]apischema.Peer, error) {
	result, err := ListPeers(ctx, req)
	return peersToAPI(result), err
}

func handleAddPeer(ctx context.Context, req apischema.InterfaceNameRequest) error {
	_, err := AddPeer(ctx, req)
	return err
}

func handleRemovePeer(ctx context.Context, req apischema.InterfaceNamePeerNameRequest) error {
	_, err := RemovePeerByName(ctx, req)
	return err
}

func handlePeerQRCode(ctx context.Context, req apischema.InterfaceNamePeerNameRequest) (apischema.QRCodeResponse, error) {
	return PeerQRCode(ctx, req)
}

func handlePeerConfigDownload(ctx context.Context, req apischema.InterfaceNamePeerNameRequest) (apischema.PeerConfigDownload, error) {
	return PeerConfigDownload(ctx, req)
}

func handleUpInterface(ctx context.Context, req apischema.NameRequest) error {
	_, err := UpInterface(ctx, req)
	return err
}

func handleDownInterface(ctx context.Context, req apischema.NameRequest) error {
	_, err := DownInterface(ctx, req)
	return err
}

func handleEnableInterface(ctx context.Context, req apischema.NameRequest) error {
	_, err := EnableInterface(ctx, req)
	return err
}

func handleDisableInterface(ctx context.Context, req apischema.NameRequest) error {
	_, err := DisableInterface(ctx, req)
	return err
}
