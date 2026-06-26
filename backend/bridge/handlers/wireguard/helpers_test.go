package wireguard

import (
	"strings"
	"testing"
)

func TestGeneratePeersAllocatesSequentialHostOffsets(t *testing.T) {
	peers, err := generatePeers("10.7.0.1/29", 3)
	if err != nil {
		t.Fatalf("generatePeers returned error: %v", err)
	}

	wantIPs := []string{"10.7.0.2/32", "10.7.0.3/32", "10.7.0.4/32"}
	wantNames := []string{"Peer2", "Peer3", "Peer4"}
	for i, peer := range peers {
		if peer.Name != wantNames[i] {
			t.Fatalf("peer %d name = %q, want %q", i, peer.Name, wantNames[i])
		}
		if len(peer.AllowedIPs) != 1 || peer.AllowedIPs[0] != wantIPs[i] {
			t.Fatalf("peer %d allowed IPs = %v, want [%s]", i, peer.AllowedIPs, wantIPs[i])
		}
		if peer.PersistentKeepalive != defaultKeepalive {
			t.Fatalf("peer %d keepalive = %d, want %d", i, peer.PersistentKeepalive, defaultKeepalive)
		}
		if peer.PrivateKey == "" || peer.PublicKey == "" {
			t.Fatalf("peer %d missing generated keys", i)
		}
	}
}

func TestGeneratePeersReturnsErrorWhenSubnetFull(t *testing.T) {
	_, err := generatePeers("10.7.0.1/30", 2)
	if err == nil {
		t.Fatal("generatePeers returned nil error")
	}
	if !strings.Contains(err.Error(), "allocate IP for peer 2") {
		t.Fatalf("error = %q, want peer allocation context", err)
	}
}
