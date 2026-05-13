package wireguard

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers wireguard handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	bridgeipc.RegisterRoutes(router, "wireguard", []bridgeipc.Command{
		{Name: "list_interfaces", Mode: bridgeipc.ModeQuery, Handler: handleListInterfaces},
		{Name: "add_interface", Mode: bridgeipc.ModeJob, Handler: handleAddInterface},
		{Name: "remove_interface", Mode: bridgeipc.ModeJob, Handler: handleRemoveInterface},
		{Name: "list_peers", Mode: bridgeipc.ModeQuery, Handler: handleListPeers},
		{Name: "add_peer", Mode: bridgeipc.ModeJob, Handler: handleAddPeer},
		{Name: "remove_peer", Mode: bridgeipc.ModeJob, Handler: handleRemovePeer},
		{Name: "peer_qrcode", Mode: bridgeipc.ModeQuery, Handler: handlePeerQRCode},
		{Name: "peer_config_download", Mode: bridgeipc.ModeQuery, Handler: handlePeerConfigDownload},
		{Name: "up_interface", Mode: bridgeipc.ModeJob, Handler: handleUpInterface},
		{Name: "down_interface", Mode: bridgeipc.ModeJob, Handler: handleDownInterface},
		{Name: "enable_interface", Mode: bridgeipc.ModeJob, Handler: handleEnableInterface},
		{Name: "disable_interface", Mode: bridgeipc.ModeJob, Handler: handleDisableInterface},
	})
}

func handleListInterfaces(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListInterfaces(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleAddInterface(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := AddInterface(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleRemoveInterface(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := RemoveInterface(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleListPeers(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListPeers(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleAddPeer(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := AddPeer(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleRemovePeer(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := RemovePeerByName(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handlePeerQRCode(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := PeerQRCode(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handlePeerConfigDownload(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := PeerConfigDownload(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleUpInterface(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := UpInterface(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleDownInterface(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := DownInterface(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleEnableInterface(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := EnableInterface(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleDisableInterface(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := DisableInterface(args)
	return bridgeipc.EmitResult(emit, result, err)
}
