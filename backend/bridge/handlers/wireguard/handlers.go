package wireguard

import "github.com/mordilloSan/LinuxIO/backend/common/ipc"

func WireguardHandlers() map[string]ipc.HandlerFunc {
	return map[string]ipc.HandlerFunc{
		"list_interfaces":      ipc.WrapSimpleHandler(ListInterfaces),
		"add_interface":        ipc.WrapSimpleHandler(AddInterface),
		"remove_interface":     ipc.WrapSimpleHandler(RemoveInterface),
		"list_peers":           ipc.WrapSimpleHandler(ListPeers),
		"add_peer":             ipc.WrapSimpleHandler(AddPeer),
		"remove_peer":          ipc.WrapSimpleHandler(RemovePeerByName),
		"peer_qrcode":          ipc.WrapSimpleHandler(PeerQRCode),
		"peer_config_download": ipc.WrapSimpleHandler(PeerConfigDownload),
		"get_keys":             ipc.WrapSimpleHandler(GetKeys),
		"up_interface":         ipc.WrapSimpleHandler(UpInterface),
		"down_interface":       ipc.WrapSimpleHandler(DownInterface),
	}
}
