package wireguard

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestAddNATHooksAddsExpectedPostUpAndPostDown(t *testing.T) {
	cfg := WireGuardConfig{
		PostUp:   []string{"echo custom up"},
		PostDown: []string{"echo custom down"},
	}

	if !addNATHooks(&cfg, "eth0", "10.7.0.1/24") {
		t.Fatal("addNATHooks reported no change")
	}

	for _, hook := range natPostUpHooks("eth0", "10.7.0.1/24") {
		if !slices.Contains(cfg.PostUp, hook) {
			t.Fatalf("PostUp missing %q in %v", hook, cfg.PostUp)
		}
	}
	for _, hook := range natPostDownHooks("eth0", "10.7.0.1/24") {
		if !slices.Contains(cfg.PostDown, hook) {
			t.Fatalf("PostDown missing %q in %v", hook, cfg.PostDown)
		}
	}
	if !slices.Contains(cfg.PostUp, "echo custom up") || !slices.Contains(cfg.PostDown, "echo custom down") {
		t.Fatalf("custom hooks were not preserved: PostUp=%v PostDown=%v", cfg.PostUp, cfg.PostDown)
	}
	if addNATHooks(&cfg, "eth0", "10.7.0.1/24") {
		t.Fatal("addNATHooks reported change on second call")
	}
}

func TestWriteWireGuardConfigPersistsPostHooks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wg0.conf")
	cfg := WireGuardConfig{
		PrivateKey: "server-private-key",
		Address:    []string{"10.7.0.1/24"},
		ListenPort: 51820,
		PostUp:     natPostUpHooks("eth0", "10.7.0.1/24"),
		PostDown:   natPostDownHooks("eth0", "10.7.0.1/24"),
	}

	if err := WriteWireGuardConfig(path, cfg); err != nil {
		t.Fatalf("WriteWireGuardConfig returned error: %v", err)
	}

	got, err := ParseWireGuardConfig(path)
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	if !slices.Equal(got.PostUp, cfg.PostUp) {
		t.Fatalf("PostUp = %v, want %v", got.PostUp, cfg.PostUp)
	}
	if !slices.Equal(got.PostDown, cfg.PostDown) {
		t.Fatalf("PostDown = %v, want %v", got.PostDown, cfg.PostDown)
	}
}

func TestPeerFileServerSectionDerivesKeysAndAllowedIPs(t *testing.T) {
	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	p := peerFile{
		ID: "Peer2", Name: "Alice", Enabled: true,
		ServerAllowedIPs:    []string{"192.168.50.0/24"},
		PrivateKey:          priv.String(),
		Address:             "10.10.20.2/32",
		PresharedKey:        "psk",
		PersistentKeepalive: 25,
	}

	section, err := p.serverSection()
	if err != nil {
		t.Fatalf("serverSection returned error: %v", err)
	}
	if section.PublicKey != priv.PublicKey().String() || section.PresharedKey != "psk" || section.PersistentKeepalive != 25 || section.Name != "Alice" {
		t.Fatalf("section = %+v", section)
	}
	if !slices.Equal(section.AllowedIPs, []string{"10.10.20.2/32", "192.168.50.0/24"}) {
		t.Fatalf("allowed ips = %v", section.AllowedIPs)
	}
}

func TestReplacePeerSectionMatchesByPublicKey(t *testing.T) {
	cfg := WireGuardConfig{Peers: []PeerConfig{
		{PublicKey: "a", AllowedIPs: []string{"10.0.0.2/32"}},
		{PublicKey: "b", AllowedIPs: []string{"10.0.0.3/32"}},
	}}

	if !replacePeerSection(&cfg, "a", nil) {
		t.Fatal("removing a known peer reported no match")
	}
	if len(cfg.Peers) != 1 || cfg.Peers[0].PublicKey != "b" {
		t.Fatalf("peers after remove = %+v", cfg.Peers)
	}

	if replacePeerSection(&cfg, "a", &PeerConfig{PublicKey: "a2", AllowedIPs: []string{"10.0.0.2/32"}}) {
		t.Fatal("replacing an absent peer reported a match")
	}
	if len(cfg.Peers) != 2 || cfg.Peers[1].PublicKey != "a2" {
		t.Fatalf("peers after append = %+v", cfg.Peers)
	}

	if !replacePeerSection(&cfg, "b", &PeerConfig{PublicKey: "b", AllowedIPs: []string{"10.0.0.3/32", "172.16.0.0/24"}}) {
		t.Fatal("replacing a known peer reported no match")
	}
	if len(cfg.Peers) != 2 || !slices.Equal(cfg.Peers[1].AllowedIPs, []string{"10.0.0.3/32", "172.16.0.0/24"}) {
		t.Fatalf("peers after replace = %+v", cfg.Peers)
	}
}

func TestWriteWireGuardConfigWritesPeerNameAsComment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wg0.conf")
	cfg := WireGuardConfig{
		PrivateKey: "server-private-key",
		Address:    []string{"10.7.0.1/24"},
		Peers:      []PeerConfig{{PublicKey: "pub", AllowedIPs: []string{"10.7.0.2/32"}, Name: "Alice's phone"}},
	}
	if err := WriteWireGuardConfig(path, cfg); err != nil {
		t.Fatalf("WriteWireGuardConfig returned error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "# Alice's phone\n[Peer]") {
		t.Fatalf("name comment missing:\n%s", raw)
	}
	if strings.Contains(string(raw), "Name") {
		t.Fatalf("a Name key would break wg setconf:\n%s", raw)
	}

	got, err := ParseWireGuardConfig(path)
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	if len(got.Peers) != 1 || got.Peers[0].Name != "" {
		t.Fatalf("parsed peers = %+v, want name left empty (metadata lives in the peer file)", got.Peers)
	}
}

func TestWireGuardConfigHeaderRoundTripsHostAndDNS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wg0.conf")
	cfg := WireGuardConfig{
		PrivateKey: "server-private-key",
		Address:    []string{"10.7.0.1/24"},
		ListenPort: 51820,
		Host:       "vpn.example.org",
		DNS:        []string{"1.1.1.1", "9.9.9.9"},
	}
	if err := WriteWireGuardConfig(path, cfg); err != nil {
		t.Fatalf("WriteWireGuardConfig returned error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "# linuxio-host: vpn.example.org\n# linuxio-dns: 1.1.1.1, 9.9.9.9\n[Interface]") {
		t.Fatalf("unexpected header:\n%s", raw)
	}
	if strings.Contains(string(raw), "DNS =") {
		t.Fatalf("client DNS must not become a wg-quick DNS key:\n%s", raw)
	}

	got, err := ParseWireGuardConfig(path)
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	if got.Host != "vpn.example.org" || !slices.Equal(got.DNS, []string{"1.1.1.1", "9.9.9.9"}) {
		t.Fatalf("parsed header = host %q dns %v", got.Host, got.DNS)
	}
}

func TestParseWireGuardConfigIgnoresServerDNSKeyAndMissingHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wg0.conf")
	src := "[Interface]\nPrivateKey = k\nAddress = 10.7.0.1/24\nDNS = 10.0.0.53\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ParseWireGuardConfig(path)
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	if got.Host != "" || len(got.DNS) != 0 {
		t.Fatalf("header must be empty: host %q dns %v", got.Host, got.DNS)
	}
}

func TestWireGuardConfigRoundTripsAllHookKinds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wg0.conf")
	cfg := WireGuardConfig{
		PrivateKey: "server-private-key",
		Address:    []string{"10.7.0.1/24"},
		PreUp:      []string{"echo pre up 1", "echo pre up 2"},
		PostUp:     []string{"echo post up"},
		PreDown:    []string{"echo pre down"},
		PostDown:   []string{"echo post down 1", "echo post down 2"},
	}
	if err := WriteWireGuardConfig(path, cfg); err != nil {
		t.Fatalf("WriteWireGuardConfig returned error: %v", err)
	}
	got, err := ParseWireGuardConfig(path)
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	if !slices.Equal(got.PreUp, cfg.PreUp) || !slices.Equal(got.PostUp, cfg.PostUp) || !slices.Equal(got.PreDown, cfg.PreDown) || !slices.Equal(got.PostDown, cfg.PostDown) {
		t.Fatalf("hooks = %+v", got)
	}
	raw, _ := os.ReadFile(path)
	if strings.Index(string(raw), "PreUp") > strings.Index(string(raw), "PostUp") || strings.Index(string(raw), "PostUp") > strings.Index(string(raw), "PreDown") {
		t.Fatalf("hook order should be PreUp, PostUp, PreDown, PostDown:\n%s", raw)
	}
}
