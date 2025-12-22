package wireguard

import "github.com/mordilloSan/LinuxIO/backend/common/ipc"

func WireguardHandlers() map[string]ipc.HandlerFunc {
	return map[string]ipc.HandlerFunc{
		"list_interfaces":      ListInterfaces,
		"add_interface":        AddInterface,
		"remove_interface":     RemoveInterface,
		"list_peers":           ListPeers,
		"add_peer":             AddPeer,
		"remove_peer":          RemovePeerByName,
		"peer_qrcode":          PeerQRCode,
		"peer_config_download": PeerConfigDownload,
		"get_keys":             GetKeys,
		"up_interface":         UpInterface,
		"down_interface":       DownInterface,
	}
}
