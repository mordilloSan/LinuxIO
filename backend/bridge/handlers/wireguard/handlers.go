package wireguard

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
)

// Every NoResponse route here is bound with HandleVoid. Their domain functions
// return `(any, error)` and were emitting real payloads — status maps, command
// output, `"removed"`, and in add_interface's case the generated *private key* —
// onto routes whose generated TypeScript is `void`. Every consumer discards the
// result (`success: (_result: void, variables) => ...`), so the payloads were
// unreachable by design and, for routes run as tasks, sat in the task snapshot
// for DefaultTerminalTaskTTL where tasks.get/tasks.list could read them back.
var api = apischema.Bindings(
	apischema.Call[apischema.NoRequest, []apischema.WireGuardInterface]("wireguard.list_interfaces", apischema.RetrySafe()).Handle(handleListInterfaces),
	apischema.MutationVoid("wireguard.add_interface", handleAddInterface),
	apischema.MutationVoid("wireguard.update_interface", UpdateInterface),
	apischema.MutationVoid("wireguard.remove_interface", handleRemoveInterface),
	apischema.Call[apischema.InterfaceNameRequest, []apischema.Peer]("wireguard.list_peers", apischema.RetrySafe()).Handle(handleListPeers),
	apischema.MutationVoid("wireguard.add_peer", handleAddPeer),
	apischema.MutationVoid("wireguard.update_peer", UpdatePeer),
	apischema.MutationVoid("wireguard.remove_peer", RemovePeer),
	apischema.Call[apischema.WireGuardPeerRequest, apischema.QRCodeResponse]("wireguard.peer_qrcode", apischema.RetrySafe()).Handle(PeerQRCode),
	apischema.Call[apischema.WireGuardPeerRequest, apischema.PeerConfigDownload]("wireguard.peer_config_download", apischema.RetrySafe()).Handle(PeerConfigDownload),
	apischema.MutationVoid("wireguard.up_interface", handleUpInterface),
	apischema.MutationVoid("wireguard.down_interface", handleDownInterface),
	apischema.MutationVoid("wireguard.enable_interface", handleEnableInterface),
	apischema.MutationVoid("wireguard.disable_interface", handleDisableInterface),
)

var Routes = api.Routes()

// RegisterHandlers registers wireguard handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleListInterfaces(ctx context.Context, _ apischema.NoRequest) ([]apischema.WireGuardInterface, error) {
	return ListInterfaces(ctx)
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

func handleAddPeer(ctx context.Context, req apischema.WireGuardAddPeerRequest) error {
	_, err := AddPeer(ctx, req)
	return err
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
