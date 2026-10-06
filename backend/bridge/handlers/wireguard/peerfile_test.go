package wireguard

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestReadPeerFileParsesMetadataHeader(t *testing.T) {
	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Peer2.conf")
	src := "# linuxio-name: Alice's phone\n" +
		"# linuxio-enabled: false\n" +
		"# linuxio-server-allowed-ips: 192.168.50.0/24, 10.9.0.0/16\n" +
		"[Interface]\nPrivateKey = " + priv.String() + "\nAddress = 10.10.20.2/32\nDNS = 1.1.1.1, 9.9.9.9\nMTU = 1420\n\n" +
		"[Peer]\nPublicKey = serverpub\nPresharedKey = psk\nAllowedIPs = 10.10.20.0/24, 192.168.1.0/24\nEndpoint = 203.0.113.10:51820\nPersistentKeepalive = 25\n"
	if err = os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := readPeerFile(path)
	if err != nil {
		t.Fatalf("readPeerFile returned error: %v", err)
	}
	if got.ID != "Peer2" || got.Name != "Alice's phone" || got.Enabled {
		t.Fatalf("identity = %q/%q enabled=%v", got.ID, got.Name, got.Enabled)
	}
	if !slices.Equal(got.ServerAllowedIPs, []string{"192.168.50.0/24", "10.9.0.0/16"}) {
		t.Fatalf("server allowed ips = %v", got.ServerAllowedIPs)
	}
	if got.PrivateKey != priv.String() || got.Address != "10.10.20.2/32" || got.MTU != 1420 {
		t.Fatalf("interface fields = %+v", got)
	}
	if !slices.Equal(got.DNS, []string{"1.1.1.1", "9.9.9.9"}) {
		t.Fatalf("dns = %v", got.DNS)
	}
	if !slices.Equal(got.ClientAllowedIPs, []string{"10.10.20.0/24", "192.168.1.0/24"}) {
		t.Fatalf("client allowed ips = %v", got.ClientAllowedIPs)
	}
	if got.PresharedKey != "psk" || got.PersistentKeepalive != 25 || got.Endpoint != "203.0.113.10:51820" || got.ServerPublicKey != "serverpub" {
		t.Fatalf("peer fields = %+v", got)
	}
	pub, err := got.publicKey()
	if err != nil {
		t.Fatalf("publicKey returned error: %v", err)
	}
	if pub != priv.PublicKey().String() {
		t.Fatalf("public key = %q, want %q", pub, priv.PublicKey().String())
	}
}

func TestReadPeerFileLegacyWithoutHeaderIsEnabledAndNamedByID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Peer7.conf")
	src := "[Interface]\nPrivateKey = key\nAddress = 10.10.20.7/32\n\n[Peer]\nPublicKey = serverpub\nAllowedIPs = 0.0.0.0/0\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := readPeerFile(path)
	if err != nil {
		t.Fatalf("readPeerFile returned error: %v", err)
	}
	if got.ID != "Peer7" || got.Name != "Peer7" || !got.Enabled || len(got.ServerAllowedIPs) != 0 {
		t.Fatalf("legacy peer = %+v", got)
	}
}

func TestWritePeerFileRoundTripsHeaderAndFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Peer3.conf")
	want := peerFile{
		ID:                  "Peer3",
		Name:                "Bob's laptop",
		Enabled:             false,
		ServerAllowedIPs:    []string{"172.16.0.0/24"},
		PrivateKey:          "priv",
		Address:             "10.10.20.3/32",
		DNS:                 []string{"1.1.1.1"},
		MTU:                 1280,
		ClientAllowedIPs:    []string{"0.0.0.0/0", "::/0"},
		PresharedKey:        "psk",
		PersistentKeepalive: 25,
		Endpoint:            "vpn.example.org:51820",
		ServerPublicKey:     "serverpub",
	}

	if err := writePeerFile(path, want); err != nil {
		t.Fatalf("writePeerFile returned error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "# linuxio-name: Bob's laptop\n# linuxio-enabled: false\n# linuxio-server-allowed-ips: 172.16.0.0/24\n[Interface]") {
		t.Fatalf("unexpected header:\n%s", raw)
	}

	got, err := readPeerFile(path)
	if err != nil {
		t.Fatalf("readPeerFile returned error: %v", err)
	}
	if got.ID != want.ID || got.Name != want.Name || got.Enabled != want.Enabled ||
		!slices.Equal(got.ServerAllowedIPs, want.ServerAllowedIPs) ||
		got.PrivateKey != want.PrivateKey || got.Address != want.Address ||
		!slices.Equal(got.DNS, want.DNS) || got.MTU != want.MTU ||
		!slices.Equal(got.ClientAllowedIPs, want.ClientAllowedIPs) ||
		got.PresharedKey != want.PresharedKey || got.PersistentKeepalive != want.PersistentKeepalive ||
		got.Endpoint != want.Endpoint || got.ServerPublicKey != want.ServerPublicKey {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestWritePeerFileOmitsEmptyOptionalLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Peer4.conf")
	err := writePeerFile(path, peerFile{
		ID: "Peer4", Name: "Peer4", Enabled: true,
		PrivateKey: "priv", Address: "10.10.20.4/32",
		ClientAllowedIPs: []string{"0.0.0.0/0"}, ServerPublicKey: "serverpub",
	})
	if err != nil {
		t.Fatalf("writePeerFile returned error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"DNS", "MTU", "PresharedKey", "Endpoint", "PersistentKeepalive", "linuxio-server-allowed-ips"} {
		if strings.Contains(string(raw), absent) {
			t.Fatalf("%s should be omitted:\n%s", absent, raw)
		}
	}
}

func TestStripPeerMetadataRemovesOnlyLinuxioLines(t *testing.T) {
	src := "# linuxio-name: x\n# keep me\n# linuxio-enabled: true\n[Interface]\nPrivateKey = k\n"
	got := stripPeerMetadata(src)
	if got != "# keep me\n[Interface]\nPrivateKey = k\n" {
		t.Fatalf("stripped = %q", got)
	}
}
