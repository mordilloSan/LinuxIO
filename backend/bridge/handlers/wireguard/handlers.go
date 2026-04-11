package wireguard

import (
	"context"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers wireguard handlers with the new handler system
func RegisterHandlers() {
	reg := func(action string, fn func([]string) (any, error)) {
		ipc.RegisterFunc("wireguard", action, func(_ context.Context, args []string, emit ipc.Events) error {
			if action != "list_interfaces" && action != "list_peers" && action != "peer_qrcode" && action != "peer_config_download" {
				logger.Infof("%s requested", action)
			}
			result, err := fn(args)
			if err != nil {
				return err
			}
			return emit.Result(result)
		})
	}

	reg("list_interfaces", ListInterfaces)
	reg("add_interface", AddInterface)
	reg("remove_interface", RemoveInterface)
	reg("list_peers", ListPeers)
	reg("add_peer", AddPeer)
	reg("remove_peer", RemovePeerByName)
	reg("peer_qrcode", PeerQRCode)
	reg("peer_config_download", PeerConfigDownload)
	reg("up_interface", UpInterface)
	reg("down_interface", DownInterface)
	reg("enable_interface", EnableInterface)
	reg("disable_interface", DisableInterface)
}
