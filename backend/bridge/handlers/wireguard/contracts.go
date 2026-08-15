package wireguard

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

func peersToAPI(values []PeerInfo) []apischema.Peer {
	result := make([]apischema.Peer, len(values))
	for i, value := range values {
		result[i] = apischema.Peer{Name: value.Name, PublicKey: value.PublicKey, AllowedIPs: value.AllowedIPs}
		if value.Endpoint != "" {
			result[i].Endpoint = &value.Endpoint
		}
		if value.PresharedKey != "" {
			result[i].PresharedKey = &value.PresharedKey
		}
		// The exported peer configuration always supplies a keepalive setting;
		// zero is the meaningful "disabled" value, not an absent value.
		result[i].PersistentKeepalive = &value.PersistentKeepalive
		if value.LastHandshake != "" {
			result[i].LastHandshake = &value.LastHandshake
		}
		if value.runtimeStatsKnown {
			result[i].LastHandshakeUnix = &value.LastHandshakeUnix
			result[i].RXBytes = &value.RxBytes
			result[i].TXBytes = &value.TxBytes
			result[i].RXBPS = &value.RxBps
			result[i].TXBPS = &value.TxBps
		}
	}
	return result
}
