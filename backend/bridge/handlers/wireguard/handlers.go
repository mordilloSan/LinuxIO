package wireguard

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
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
	logAction("add_interface")
	result, err := AddInterface(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleRemoveInterface(ctx context.Context, args []string, emit bridgeipc.Events) error {
	logAction("remove_interface")
	result, err := RemoveInterface(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleListPeers(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListPeers(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleAddPeer(ctx context.Context, args []string, emit bridgeipc.Events) error {
	logAction("add_peer")
	result, err := AddPeer(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleRemovePeer(ctx context.Context, args []string, emit bridgeipc.Events) error {
	logAction("remove_peer")
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
	logAction("up_interface")
	result, err := UpInterface(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleDownInterface(ctx context.Context, args []string, emit bridgeipc.Events) error {
	logAction("down_interface")
	result, err := DownInterface(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleEnableInterface(ctx context.Context, args []string, emit bridgeipc.Events) error {
	logAction("enable_interface")
	result, err := EnableInterface(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleDisableInterface(ctx context.Context, args []string, emit bridgeipc.Events) error {
	logAction("disable_interface")
	result, err := DisableInterface(args)
	return bridgeipc.EmitResult(emit, result, err)
}

func logAction(action string) {
	slog.Info("wireguard action requested", "component", "wireguard", "mode", action)
}
