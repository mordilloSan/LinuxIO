package wireguard

import (
	"go-backend/cmd/bridge/handlers/types"
)

func WireguardHandlers() map[string]types.HandlerFunc {
	return map[string]types.HandlerFunc{
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
