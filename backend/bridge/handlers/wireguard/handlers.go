package wireguard

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers wireguard handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime) {
	rpc.Register("wireguard", rt, []rpc.Command{
		{Name: "list_interfaces", Handler: handleListInterfaces},
		{Name: "add_interface", Handler: handleAddInterface},
		{Name: "remove_interface", Handler: handleRemoveInterface},
		{Name: "list_peers", Handler: handleListPeers},
		{Name: "add_peer", Handler: handleAddPeer},
		{Name: "remove_peer", Handler: handleRemovePeer},
		{Name: "peer_qrcode", Handler: handlePeerQRCode},
		{Name: "peer_config_download", Handler: handlePeerConfigDownload},
		{Name: "up_interface", Handler: handleUpInterface},
		{Name: "down_interface", Handler: handleDownInterface},
		{Name: "enable_interface", Handler: handleEnableInterface},
		{Name: "disable_interface", Handler: handleDisableInterface},
	})
}

func handleListInterfaces(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := ListInterfaces(args)
	return rpc.EmitResult(emit, result, err)
}

func handleAddInterface(ctx context.Context, args []string, emit ipc.Events) error {
	logAction("add_interface")
	result, err := AddInterface(args)
	return rpc.EmitResult(emit, result, err)
}

func handleRemoveInterface(ctx context.Context, args []string, emit ipc.Events) error {
	logAction("remove_interface")
	result, err := RemoveInterface(args)
	return rpc.EmitResult(emit, result, err)
}

func handleListPeers(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := ListPeers(args)
	return rpc.EmitResult(emit, result, err)
}

func handleAddPeer(ctx context.Context, args []string, emit ipc.Events) error {
	logAction("add_peer")
	result, err := AddPeer(args)
	return rpc.EmitResult(emit, result, err)
}

func handleRemovePeer(ctx context.Context, args []string, emit ipc.Events) error {
	logAction("remove_peer")
	result, err := RemovePeerByName(args)
	return rpc.EmitResult(emit, result, err)
}

func handlePeerQRCode(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := PeerQRCode(args)
	return rpc.EmitResult(emit, result, err)
}

func handlePeerConfigDownload(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := PeerConfigDownload(args)
	return rpc.EmitResult(emit, result, err)
}

func handleUpInterface(ctx context.Context, args []string, emit ipc.Events) error {
	logAction("up_interface")
	result, err := UpInterface(args)
	return rpc.EmitResult(emit, result, err)
}

func handleDownInterface(ctx context.Context, args []string, emit ipc.Events) error {
	logAction("down_interface")
	result, err := DownInterface(args)
	return rpc.EmitResult(emit, result, err)
}

func handleEnableInterface(ctx context.Context, args []string, emit ipc.Events) error {
	logAction("enable_interface")
	result, err := EnableInterface(args)
	return rpc.EmitResult(emit, result, err)
}

func handleDisableInterface(ctx context.Context, args []string, emit ipc.Events) error {
	logAction("disable_interface")
	result, err := DisableInterface(args)
	return rpc.EmitResult(emit, result, err)
}

func logAction(action string) {
	slog.Info("wireguard action requested", "component", "wireguard", "mode", action)
}
