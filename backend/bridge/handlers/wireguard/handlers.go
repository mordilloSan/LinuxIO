package wireguard

import (
	"context"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers wireguard handlers with the new handler system
func RegisterHandlers() {
	ipc.RegisterFunc("wireguard", "list_interfaces", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := ListInterfaces(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("wireguard", "add_interface", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Infof("add_interface requested")
		result, err := AddInterface(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("wireguard", "remove_interface", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Infof("remove_interface requested")
		result, err := RemoveInterface(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("wireguard", "list_peers", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := ListPeers(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("wireguard", "add_peer", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Infof("add_peer requested")
		result, err := AddPeer(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("wireguard", "remove_peer", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Infof("remove_peer requested")
		result, err := RemovePeerByName(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("wireguard", "peer_qrcode", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := PeerQRCode(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("wireguard", "peer_config_download", func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := PeerConfigDownload(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("wireguard", "up_interface", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Infof("up_interface requested")
		result, err := UpInterface(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("wireguard", "down_interface", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Infof("down_interface requested")
		result, err := DownInterface(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("wireguard", "enable_interface", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Infof("enable_interface requested")
		result, err := EnableInterface(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("wireguard", "disable_interface", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Infof("disable_interface requested")
		result, err := DisableInterface(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})
}
