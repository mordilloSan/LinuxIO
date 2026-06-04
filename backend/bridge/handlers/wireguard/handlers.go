package wireguard

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	wireguardapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/wireguard/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers wireguard handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: wireguardapi.ListInterfaces, Handle: handleListInterfaces},
		{Route: wireguardapi.AddInterface, Handle: handleAddInterface},
		{Route: wireguardapi.RemoveInterface, Handle: handleRemoveInterface},
		{Route: wireguardapi.ListPeers, Handle: handleListPeers},
		{Route: wireguardapi.AddPeer, Handle: handleAddPeer},
		{Route: wireguardapi.RemovePeer, Handle: handleRemovePeer},
		{Route: wireguardapi.PeerQrcode, Handle: handlePeerQRCode},
		{Route: wireguardapi.PeerConfigDownload, Handle: handlePeerConfigDownload},
		{Route: wireguardapi.UpInterface, Handle: handleUpInterface},
		{Route: wireguardapi.DownInterface, Handle: handleDownInterface},
		{Route: wireguardapi.EnableInterface, Handle: handleEnableInterface},
		{Route: wireguardapi.DisableInterface, Handle: handleDisableInterface},
	})
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
