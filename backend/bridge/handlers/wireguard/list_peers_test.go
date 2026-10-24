package wireguard

import (
	"context"
	"slices"
	"testing"
	"time"

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

func TestPruneRatesDropsMissingPeersOnly(t *testing.T) {
	t0 := time.Now()
	computeRates("wgprune", "gone", 0, 0, t0)
	computeRates("wgprune", "kept", 0, 0, t0)
	computeRates("wgprune-other", "gone", 0, 0, t0)
	t.Cleanup(func() { pruneRates("wgprune", nil); pruneRates("wgprune-other", nil) })

	pruneRates("wgprune", map[string]peerRuntimeStats{"kept": {}})

	rateMu.Lock()
	_, goneOK := rateCache["wgprune|gone"]
	_, keptOK := rateCache["wgprune|kept"]
	_, otherOK := rateCache["wgprune-other|gone"]
	rateMu.Unlock()
	if goneOK || !keptOK || !otherOK {
		t.Fatalf("cache after prune: gone=%v kept=%v other-iface=%v", goneOK, keptOK, otherOK)
	}

	rx, _ := computeRates("wgprune", "kept", 1000, 0, t0.Add(2*time.Second))
	if rx != 500 {
		t.Fatalf("kept peer rx rate = %v, want 500", rx)
	}
	if rx, _ := computeRates("wgprune", "gone", 1000, 0, t0.Add(2*time.Second)); rx != 0 {
		t.Fatalf("pruned peer rx rate = %v, want 0 (first observation)", rx)
	}
}
