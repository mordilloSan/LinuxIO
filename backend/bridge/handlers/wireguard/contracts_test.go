package wireguard

import "testing"

func TestPeersToAPIKeepsKnownZeroRuntimeStats(t *testing.T) {
	peers := peersToAPI([]PeerInfo{{
		PeerConfig:    PeerConfig{PublicKey: "public", PersistentKeepalive: 0},
		LastHandshake: "never", runtimeStatsKnown: true,
	}})
	peer := peers[0]
	if peer.PersistentKeepalive == nil || *peer.PersistentKeepalive != 0 {
		t.Fatalf("keepalive = %v, want known disabled zero", peer.PersistentKeepalive)
	}
	if peer.LastHandshakeUnix == nil || *peer.LastHandshakeUnix != 0 {
		t.Fatalf("last handshake = %v, want known zero", peer.LastHandshakeUnix)
	}
	if peer.RXBytes == nil || *peer.RXBytes != 0 || peer.TXBytes == nil || *peer.TXBytes != 0 {
		t.Fatalf("byte counters must retain known zero: %#v", peer)
	}
	if peer.RXBPS == nil || *peer.RXBPS != 0 || peer.TXBPS == nil || *peer.TXBPS != 0 {
		t.Fatalf("rates must retain known zero: %#v", peer)
	}
}

func TestPeersToAPIOmitsUnavailableRuntimeStats(t *testing.T) {
	peers := peersToAPI([]PeerInfo{{
		PeerConfig:    PeerConfig{PublicKey: "public", PersistentKeepalive: 0},
		LastHandshake: "never",
	}})
	peer := peers[0]
	if peer.PersistentKeepalive == nil || *peer.PersistentKeepalive != 0 {
		t.Fatalf("configured disabled keepalive must remain present: %v", peer.PersistentKeepalive)
	}
	if peer.LastHandshakeUnix != nil || peer.RXBytes != nil || peer.TXBytes != nil || peer.RXBPS != nil || peer.TXBPS != nil {
		t.Fatalf("unavailable runtime stats must be omitted: %#v", peer)
	}
	if peer.LastHandshake == nil || *peer.LastHandshake != "never" {
		t.Fatalf("legacy handshake string must remain visible: %v", peer.LastHandshake)
	}
}
