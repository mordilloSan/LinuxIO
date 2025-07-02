package wireguard

import (
	"go-backend/cmd/bridge/handlers/types"
)

func WireguardHandlers() map[string]types.HandlerFunc {
	return map[string]types.HandlerFunc{
		"list_interfaces":  ListInterfaces,
		"get_interface":    GetInterface,
		"add_interface":    AddInterface,
		"remove_interface": RemoveInterface,
		"list_peers":       ListPeers,
		"add_peer":         AddPeer,
		"remove_peer":      RemovePeer,
		"get_keys":         GetKeys,
	}
}
