package wireguard

import (
	"context"
	"os"
	"slices"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

type wgQuickCall struct {
	action string
	name   string
}

type wireGuardTestEnv struct {
	dir     string
	wgQuick []wgQuickCall
	// wgQuickPorts[i] is the listen port found on disk when wgQuick[i] ran,
	// so tests can prove "down" saw the old config and "up" the new one.
	wgQuickPorts []int
	syncs        []string
	interfaceUp  bool
	publicIP     string
}

func newWireGuardTestEnv(t *testing.T) *wireGuardTestEnv {
	t.Helper()

	env := &wireGuardTestEnv{dir: t.TempDir(), publicIP: "203.0.113.10"}
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
		port := 0
		if cfg, err := ParseWireGuardConfig(configPath(name)); err == nil {
			port = cfg.ListenPort
		}
		env.wgQuickPorts = append(env.wgQuickPorts, port)
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
		return env.publicIP, nil
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

func writeTestInterface(t *testing.T, name, address string) WireGuardConfig {
	t.Helper()
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey returned error: %v", err)
	}
	cfg := WireGuardConfig{PrivateKey: privateKey.String(), Address: []string{address}, ListenPort: 51820}
	addNATHooks(&cfg, "eth0", address)
	if err := WriteWireGuardConfig(configPath(name), cfg); err != nil {
		t.Fatalf("WriteWireGuardConfig returned error: %v", err)
	}
	return cfg
}

// setTestInterfaceHeader rewrites the interface config with the given host and
// client DNS in its LinuxIO header.
func setTestInterfaceHeader(t *testing.T, name, host string, dns []string) {
	t.Helper()
	cfg, err := ParseWireGuardConfig(configPath(name))
	if err != nil {
		t.Fatalf("ParseWireGuardConfig returned error: %v", err)
	}
	cfg.Host = host
	cfg.DNS = dns
	if err := WriteWireGuardConfig(configPath(name), cfg); err != nil {
		t.Fatalf("WriteWireGuardConfig returned error: %v", err)
	}
}

func setupTwoPeers(t *testing.T) (*wireGuardTestEnv, string, string) {
	t.Helper()
	env, first := setupPeer(t)
	second, err := AddPeer(context.Background(), apischema.WireGuardAddPeerRequest{InterfaceName: "wgtest", Name: "Bob"})
	if err != nil {
		t.Fatalf("AddPeer returned error: %v", err)
	}
	updatePeer(t, apischema.WireGuardUpdatePeerRequest{PeerID: second, Enabled: new(false), ServerAllowedIPs: []string{"172.16.0.0/24"}})
	env.syncs = nil
	return env, first, second
}
