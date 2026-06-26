package wireguard

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type wgQuickCall struct {
	action string
	name   string
}

type wireGuardTestEnv struct {
	dir         string
	wgQuick     []wgQuickCall
	syncs       []string
	interfaceUp bool
}

func newWireGuardTestEnv(t *testing.T) *wireGuardTestEnv {
	t.Helper()

	env := &wireGuardTestEnv{dir: t.TempDir()}
	oldConfigDir := wgConfigDir
	oldRunWGQuick := runWGQuickCommand
	oldSyncConfig := syncWireGuardConfigFunc
	oldIsInterfaceUp := isInterfaceUpFunc
	oldPublicIP := getPublicIPFunc
	oldDefaultGateway := getDefaultGatewayIPv4Func
	oldInterfaceGateway := getGatewayForInterfaceIPv4Func

	wgConfigDir = env.dir
	runWGQuickCommand = func(_ context.Context, action, name string) (string, error) {
		env.wgQuick = append(env.wgQuick, wgQuickCall{action: action, name: name})
		return action + " ok", nil
	}
	syncWireGuardConfigFunc = func(_ context.Context, name string) (string, error) {
		env.syncs = append(env.syncs, name)
		return "sync ok", nil
	}
	isInterfaceUpFunc = func(string) bool {
		return env.interfaceUp
	}
	getPublicIPFunc = func() (string, error) {
		return "203.0.113.10", nil
	}
	getDefaultGatewayIPv4Func = func() (string, error) {
		return "192.0.2.1", nil
	}
	getGatewayForInterfaceIPv4Func = func(string) (string, error) {
		return "192.0.2.1", nil
	}

	t.Cleanup(func() {
		wgConfigDir = oldConfigDir
		runWGQuickCommand = oldRunWGQuick
		syncWireGuardConfigFunc = oldSyncConfig
		isInterfaceUpFunc = oldIsInterfaceUp
		getPublicIPFunc = oldPublicIP
		getDefaultGatewayIPv4Func = oldDefaultGateway
		getGatewayForInterfaceIPv4Func = oldInterfaceGateway
	})

	return env
}

func TestAddAndRemoveInterfaceLifecycle(t *testing.T) {
	env := newWireGuardTestEnv(t)
	dns := "1.1.1.1"
	numPeers := "1"

	_, err := AddInterface(context.Background(), apischema.WireGuardAddInterfaceRequest{
		Name:       "wgtest",
		Addresses:  "10.7.0.1/24",
		ListenPort: "51820",
		EgressNic:  "eth0",
		DNS:        &dns,
		NumPeers:   &numPeers,
	})
	if err != nil {
		t.Fatalf("AddInterface returned error: %v", err)
	}

	cfg, err := ParseWireGuardConfig(configPath("wgtest"))
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	assertNATHooks(t, cfg, "eth0", "10.7.0.1/24")
	if len(cfg.Peers) != 1 {
		t.Fatalf("peer count = %d, want 1", len(cfg.Peers))
	}
	assertPathExists(t, peerConfigPath("wgtest", "Peer2"))
	if !slices.Equal(env.wgQuick, []wgQuickCall{{action: "up", name: "wgtest"}}) {
		t.Fatalf("wg-quick calls = %v", env.wgQuick)
	}

	_, err = RemoveInterface(context.Background(), apischema.NameRequest{Name: "wgtest"})
	if err != nil {
		t.Fatalf("RemoveInterface returned error: %v", err)
	}
	assertPathMissing(t, configPath("wgtest"))
	assertPathMissing(t, peerDirPath("wgtest"))
	wantWGQuick := []wgQuickCall{{action: "up", name: "wgtest"}, {action: "down", name: "wgtest"}}
	if !slices.Equal(env.wgQuick, wantWGQuick) {
		t.Fatalf("wg-quick calls = %v, want %v", env.wgQuick, wantWGQuick)
	}
}

func TestAddAndRemovePeerLifecyclePreservesNATHooks(t *testing.T) {
	env := newWireGuardTestEnv(t)
	env.interfaceUp = true
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey returned error: %v", err)
	}

	cfg := WireGuardConfig{
		PrivateKey: privateKey.String(),
		Address:    []string{"10.8.0.1/29"},
		ListenPort: 51820,
	}
	addNATHooks(&cfg, "eth0", "10.8.0.1/29")
	err = WriteWireGuardConfig(configPath("wgtest"), cfg)
	if err != nil {
		t.Fatalf("WriteWireGuardConfig returned error: %v", err)
	}

	result, err := AddPeer(context.Background(), apischema.InterfaceNameRequest{InterfaceName: "wgtest"})
	if err != nil {
		t.Fatalf("AddPeer returned error: %v", err)
	}
	added, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("AddPeer result = %T, want map[string]any", result)
	}
	peerName, ok := added["peer_name"].(string)
	if !ok || peerName == "" {
		t.Fatalf("AddPeer peer_name = %#v", added["peer_name"])
	}

	cfg, err = ParseWireGuardConfig(configPath("wgtest"))
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	if len(cfg.Peers) != 1 {
		t.Fatalf("peer count after add = %d, want 1", len(cfg.Peers))
	}
	assertNATHooks(t, cfg, "eth0", "10.8.0.1/29")
	peerPath := filepath.Join(peerDirPath("wgtest"), peerName+configExt)
	assertPathExists(t, peerPath)
	if !slices.Equal(env.syncs, []string{"wgtest"}) {
		t.Fatalf("syncs after add = %v, want [wgtest]", env.syncs)
	}

	_, err = RemovePeerByName(context.Background(), apischema.InterfaceNamePeerNameRequest{
		InterfaceName: "wgtest",
		PeerName:      peerName,
	})
	if err != nil {
		t.Fatalf("RemovePeerByName returned error: %v", err)
	}

	cfg, err = ParseWireGuardConfig(configPath("wgtest"))
	if err != nil {
		t.Fatalf("ParseWireGuardConfig after remove returned error: %v", err)
	}
	if len(cfg.Peers) != 0 {
		t.Fatalf("peer count after remove = %d, want 0", len(cfg.Peers))
	}
	assertNATHooks(t, cfg, "eth0", "10.8.0.1/29")
	assertPathMissing(t, peerPath)
	if !slices.Equal(env.syncs, []string{"wgtest", "wgtest"}) {
		t.Fatalf("syncs after remove = %v, want [wgtest wgtest]", env.syncs)
	}
}

func TestUpAndDownInterfaceRunWGQuick(t *testing.T) {
	env := newWireGuardTestEnv(t)

	up, err := UpInterface(context.Background(), apischema.NameRequest{Name: "wgtest"})
	if err != nil {
		t.Fatalf("UpInterface returned error: %v", err)
	}
	upResult, ok := up.(map[string]any)
	if !ok || upResult["status"] != "on" {
		t.Fatalf("UpInterface result = %#v, want status on", up)
	}

	down, err := DownInterface(context.Background(), apischema.NameRequest{Name: "wgtest"})
	if err != nil {
		t.Fatalf("DownInterface returned error: %v", err)
	}
	downResult, ok := down.(map[string]any)
	if !ok || downResult["status"] != "off" {
		t.Fatalf("DownInterface result = %#v, want status off", down)
	}

	want := []wgQuickCall{{action: "up", name: "wgtest"}, {action: "down", name: "wgtest"}}
	if !slices.Equal(env.wgQuick, want) {
		t.Fatalf("wg-quick calls = %v, want %v", env.wgQuick, want)
	}
}

func assertNATHooks(t *testing.T, cfg WireGuardConfig, egressNic, subnet string) {
	t.Helper()

	for _, hook := range natPostUpHooks(egressNic, subnet) {
		if !slices.Contains(cfg.PostUp, hook) {
			t.Fatalf("PostUp missing %q in %v", hook, cfg.PostUp)
		}
	}
	for _, hook := range natPostDownHooks(egressNic, subnet) {
		if !slices.Contains(cfg.PostDown, hook) {
			t.Fatalf("PostDown missing %q in %v", hook, cfg.PostDown)
		}
	}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s missing: %v", path, err)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s still exists or stat failed unexpectedly: %v", path, err)
	}
}
