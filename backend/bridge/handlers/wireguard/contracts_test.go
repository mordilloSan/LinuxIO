package wireguard

import (
	"slices"
	"testing"
)

func TestPeersToAPIKeepsKnownZeroRuntimeStats(t *testing.T) {
	peers := peersToAPI([]PeerInfo{{
		PersistentKeepalive: 0,
		PublicKey:           "public", LastHandshake: "never", runtimeStatsKnown: true,
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
		PersistentKeepalive: 0,
		PublicKey:           "public", LastHandshake: "never",
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

func TestPeersToAPIMapsMetadataAndClientSettings(t *testing.T) {
	peers := peersToAPI([]PeerInfo{{
		ID: "Peer2", Name: "Alice", Enabled: false,
		Address:          "10.0.0.2/32",
		ClientAllowedIPs: []string{"0.0.0.0/0"},
		ServerAllowedIPs: []string{"172.16.0.0/24"},
		DNS:              []string{"1.1.1.1"},
		MTU:              1400,
		PresharedKey:     "psk",
		Endpoint:         "host:51820",
		PublicKey:        "public",
	}})
	peer := peers[0]
	if peer.ID != "Peer2" || peer.Name != "Alice" || peer.Enabled || peer.Address != "10.0.0.2/32" || peer.MTU != 1400 {
		t.Fatalf("peer = %+v", peer)
	}
	if !slices.Equal(peer.ClientAllowedIPs, []string{"0.0.0.0/0"}) || !slices.Equal(peer.ServerAllowedIPs, []string{"172.16.0.0/24"}) || !slices.Equal(peer.DNS, []string{"1.1.1.1"}) {
		t.Fatalf("lists = %+v", peer)
	}
	if peer.PresharedKey == nil || *peer.PresharedKey != "psk" || peer.Endpoint == nil || *peer.Endpoint != "host:51820" {
		t.Fatalf("optional strings = %+v", peer)
	}
	empty := peersToAPI([]PeerInfo{{ID: "Peer3"}})[0]
	if empty.ServerAllowedIPs == nil || empty.DNS == nil || empty.ClientAllowedIPs == nil {
		t.Fatalf("lists must serialise as [] not null: %+v", empty)
	}
}
