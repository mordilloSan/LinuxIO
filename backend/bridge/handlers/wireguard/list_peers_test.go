package wireguard

import (
	"context"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestListPeersReportsMetadataAndDerivedKeys(t *testing.T) {
	_, first, second := setupTwoPeers(t)

	peers, err := ListPeers(context.Background(), apischema.InterfaceNameRequest{InterfaceName: "wgtest"})
	if err != nil {
		t.Fatalf("ListPeers returned error: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("peer count = %d, want 2", len(peers))
	}
	byID := map[string]PeerInfo{}
	for _, peer := range peers {
		byID[peer.ID] = peer
	}

	alice := byID[first]
	aliceFile, _ := readPeerFile(peerConfigPath("wgtest", first))
	alicePub, _ := aliceFile.publicKey()
	if alice.Name != "Alice" || !alice.Enabled || alice.Address != "10.8.0.2/32" || alice.PublicKey != alicePub {
		t.Fatalf("alice = %+v", alice)
	}
	if !slices.Equal(alice.ClientAllowedIPs, fullTunnelAllowedIPs) || !slices.Equal(alice.DNS, []string{"192.0.2.1"}) || alice.PresharedKey == "" {
		t.Fatalf("alice client settings = %+v", alice)
	}

	bob := byID[second]
	if bob.Name != "Bob" || bob.Enabled || !slices.Equal(bob.ServerAllowedIPs, []string{"172.16.0.0/24"}) {
		t.Fatalf("bob = %+v", bob)
	}
	if bob.PublicKey == "" || bob.PublicKey == alice.PublicKey {
		t.Fatalf("bob public key must be derived even while disabled: %q", bob.PublicKey)
	}
}
