package network

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vishvananda/netlink"
)

func bridgeTestEnvironment(t *testing.T, probes []InterfaceProbe) (Environment, *fakeRunner) {
	t.Helper()
	env, runner, _ := testEnv(t)
	env.InterfaceProbes = func(context.Context) ([]InterfaceProbe, error) {
		return probes, nil
	}
	env.ManagerForInterface = func(context.Context, string) (string, error) {
		return bridgeBackendNetworkd, nil
	}
	env.VerifyBridge = func(context.Context, string, string) (bool, error) {
		return true, nil
	}
	env.RemoveBridge = func(string) error { return nil }
	env.ReadFile = func(path string) ([]byte, error) {
		if path == "/proc/sys/net/bridge/bridge-nf-call-iptables" {
			return nil, os.ErrNotExist
		}
		return os.ReadFile(path)
	}
	return env, runner
}

func TestGetBridgeOptionsRejectsUnsafeMembers(t *testing.T) {
	env, _ := bridgeTestEnvironment(t, []InterfaceProbe{
		{Name: "eth0", MAC: "00:11:22:33:44:55", Ethernet: true, Addresses: []string{"192.0.2.5/24"}},
		{Name: "eth1", MAC: "00:11:22:33:44:66", Ethernet: true, Master: "bond0"},
		{Name: "wlan0", MAC: "00:11:22:33:44:77", Ethernet: false, Wireless: true},
		{Name: "eth2", MAC: "00:11:22:33:44:88", Ethernet: true},
	})

	options, err := GetBridgeOptions(context.Background(), env)
	if err != nil {
		t.Fatalf("GetBridgeOptions: %v", err)
	}
	if len(options.Candidates) != 4 {
		t.Fatalf("candidate count = %d, want 4", len(options.Candidates))
	}
	if options.Candidates[3].Name != "eth2" || !options.Candidates[3].Eligible || options.Candidates[3].Backend != bridgeBackendNetworkd {
		t.Fatalf("spare candidate = %#v", options.Candidates[3])
	}
	for _, name := range []string{"eth0", "eth1", "wlan0"} {
		for _, candidate := range options.Candidates {
			if candidate.Name == name && candidate.Eligible {
				t.Fatalf("%s unexpectedly eligible: %#v", name, candidate)
			}
		}
	}
}

func TestGetBridgeOptionsSkipsOwnershipOnlyForHardLinkBlockers(t *testing.T) {
	probes := []InterfaceProbe{
		{Name: "lo", Loopback: true},
		{Name: "eth0", Ethernet: true, Addresses: []string{"192.0.2.5/24"}},
	}
	env, _ := bridgeTestEnvironment(t, probes)
	ownerCalls := make(map[string]int)
	env.ManagerForInterface = func(_ context.Context, iface string) (string, error) {
		ownerCalls[iface]++
		return bridgeBackendNetworkd, nil
	}

	options, err := GetBridgeOptions(context.Background(), env)
	if err != nil {
		t.Fatalf("GetBridgeOptions: %v", err)
	}
	if ownerCalls["lo"] != 0 {
		t.Fatalf("hard-blocked ownership calls = %d, want 0", ownerCalls["lo"])
	}
	if ownerCalls["eth0"] == 0 {
		t.Fatal("L3-only candidate did not run ownership inspection")
	}
	if got := options.Candidates[0]; got.Backend != "" || got.Eligible || got.HandoffEligible {
		t.Fatalf("hard-blocked candidate = %#v", got)
	}
	if reasons := strings.Join(options.Candidates[0].Reasons, "; "); !strings.Contains(reasons, "loopback interface") {
		t.Fatalf("hard-blocked reasons = %q", reasons)
	}
	if reasons := strings.Join(options.Candidates[1].Reasons, "; "); !strings.Contains(reasons, "non-link-local address") {
		t.Fatalf("L3-only reasons = %q", reasons)
	}
}

func TestMasterLinkNameSkipsZeroIndex(t *testing.T) {
	calls := 0
	lookup := func(index int) (netlink.Link, error) {
		calls++
		return &netlink.Dummy{Name: "br0", Index: index}, nil
	}

	if got := masterLinkName(0, lookup); got != "" {
		t.Fatalf("zero-index master = %q", got)
	}
	if calls != 0 {
		t.Fatalf("zero-index lookup calls = %d, want 0", calls)
	}
	if got := masterLinkName(4, lookup); got != "br0" {
		t.Fatalf("nonzero master = %q", got)
	}
	if calls != 1 {
		t.Fatalf("nonzero lookup calls = %d, want 1", calls)
	}
}

func TestNetplanBridgeDeltaEncodesMemberAsData(t *testing.T) {
	member := `eth0]}#x`
	delta, err := netplanBridgeDelta(BridgePlan{Name: "br.test", Member: member})
	if err != nil {
		t.Fatalf("netplanBridgeDelta: %v", err)
	}
	key, value, found := strings.Cut(delta, "=")
	if !found || key != `bridges.br\.test` {
		t.Fatalf("delta key = %q, found = %v", key, found)
	}
	var decoded struct {
		Interfaces []string `json:"interfaces"`
		DHCP4      bool     `json:"dhcp4"`
		DHCP6      bool     `json:"dhcp6"`
		LinkLocal  []string `json:"link-local"`
	}
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		t.Fatalf("decode delta value %q: %v", value, err)
	}
	if len(decoded.Interfaces) != 1 || decoded.Interfaces[0] != member {
		t.Fatalf("interfaces = %#v", decoded.Interfaces)
	}
	if decoded.DHCP4 || decoded.DHCP6 || decoded.LinkLocal == nil || len(decoded.LinkLocal) != 0 {
		t.Fatalf("decoded value = %#v", decoded)
	}
}

func TestCreateBridgeWritesNetworkdConfigurationAndApplies(t *testing.T) {
	env, runner := bridgeTestEnvironment(t, []InterfaceProbe{{
		Name: "eth1", MAC: "00:11:22:33:44:66", Ethernet: true,
	}})

	result, err := CreateBridge(context.Background(), env, BridgePlan{Name: "br.test", Member: "eth1"})
	if err != nil {
		t.Fatalf("CreateBridge: %v", err)
	}
	if result != (BridgeResult{Name: "br.test", Member: "eth1", Backend: bridgeBackendNetworkd}) {
		t.Fatalf("result = %#v", result)
	}
	for _, path := range []string{
		filepath.Join(env.NetworkdDir, "90-linuxio-br.test.netdev"),
		filepath.Join(env.NetworkdDir, "90-linuxio-br.test.network"),
		filepath.Join(env.NetworkdDir, "90-linuxio-br.test-member.network"),
	} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("expected networkd file %s: %v", path, statErr)
		}
	}
	member, err := os.ReadFile(filepath.Join(env.NetworkdDir, "90-linuxio-br.test-member.network"))
	if err != nil {
		t.Fatalf("read member network: %v", err)
	}
	if body := string(member); !strings.Contains(body, "Bridge=br.test") || !strings.Contains(body, "DHCP=no") {
		t.Fatalf("member network = %q", body)
	}
	requireCalls(t, runner,
		"networkctl reload",
		"networkctl reconfigure br.test",
		"networkctl reconfigure eth1",
	)
}

func TestCreateBridgeUsesSinglePreflightScan(t *testing.T) {
	probes := []InterfaceProbe{{
		Name: "eth1", MAC: "00:11:22:33:44:66", Ethernet: true,
	}}
	env, _ := bridgeTestEnvironment(t, probes)
	scans := 0
	env.InterfaceProbes = func(context.Context) ([]InterfaceProbe, error) {
		scans++
		return probes, nil
	}

	if _, err := CreateBridge(context.Background(), env, BridgePlan{Name: "br.test", Member: "eth1"}); err != nil {
		t.Fatalf("CreateBridge: %v", err)
	}
	if scans != 1 {
		t.Fatalf("interface scans = %d, want 1", scans)
	}
}

func TestNetplanCandidateUsesSupportedRenderer(t *testing.T) {
	env, _ := bridgeTestEnvironment(t, []InterfaceProbe{{
		Name: "eth1", MAC: "00:11:22:33:44:66", Ethernet: true,
	}})
	mustWriteFile(t, filepath.Join(env.NetplanDir, "01-net.yaml"), `
network:
  version: 2
  renderer: networkd
  ethernets:
    eth1: {}
`)
	options, err := GetBridgeOptions(context.Background(), env)
	if err != nil {
		t.Fatalf("GetBridgeOptions: %v", err)
	}
	if len(options.Candidates) != 1 || !options.Candidates[0].Eligible || options.Candidates[0].Backend != bridgeBackendNetplan {
		t.Fatalf("Netplan candidate = %#v", options.Candidates)
	}
}

func TestNetplanCandidateRejectsNonEthernetDefinition(t *testing.T) {
	env, _ := bridgeTestEnvironment(t, []InterfaceProbe{{
		Name: "wlan0", MAC: "00:11:22:33:44:66", Ethernet: true,
	}})
	mustWriteFile(t, filepath.Join(env.NetplanDir, "01-net.yaml"), `
network:
  version: 2
  renderer: networkd
  wifis:
    wlan0: {}
`)

	options, err := GetBridgeOptions(context.Background(), env)
	if err != nil {
		t.Fatalf("GetBridgeOptions: %v", err)
	}
	if len(options.Candidates) != 1 || options.Candidates[0].Eligible {
		t.Fatalf("Wi-Fi candidate = %#v", options.Candidates)
	}
	if reason := strings.Join(options.Candidates[0].Reasons, "; "); !strings.Contains(reason, "not a physical Ethernet device") {
		t.Fatalf("Wi-Fi refusal reason = %q", reason)
	}
}

func TestNetworkdCandidateRejectsExistingL2Attachment(t *testing.T) {
	env, _ := bridgeTestEnvironment(t, []InterfaceProbe{{
		Name: "eth1", MAC: "00:11:22:33:44:66", Ethernet: true,
	}})
	mustWriteFile(t, filepath.Join(env.NetworkdDir, "10-eth1.network"), `
[Match]
Name=eth1

[Network]
DHCP=no
Bridge=br-old
`)

	options, err := GetBridgeOptions(context.Background(), env)
	if err != nil {
		t.Fatalf("GetBridgeOptions: %v", err)
	}
	if len(options.Candidates) != 1 || options.Candidates[0].Eligible {
		t.Fatalf("attached candidate = %#v", options.Candidates)
	}
	if reason := strings.Join(options.Candidates[0].Reasons, "; "); !strings.Contains(reason, "already has Bridge configuration") {
		t.Fatalf("attachment refusal reason = %q", reason)
	}
}

func TestExistingNetworkManagerProfileIsRefusedInsteadOfDuplicated(t *testing.T) {
	env, _ := bridgeTestEnvironment(t, []InterfaceProbe{{
		Name: "eth1", MAC: "00:11:22:33:44:66", Ethernet: true,
	}})
	env.ManagerForInterface = func(context.Context, string) (string, error) {
		return bridgeBackendNetworkManager, nil
	}
	mustWriteFile(t, filepath.Join(env.NMConnectionDir, "eth1.nmconnection"), `
[connection]
id=eth1
type=802-3-ethernet
interface-name=eth1

[ipv4]
method=disabled

[ipv6]
method=disabled
`)

	options, err := GetBridgeOptions(context.Background(), env)
	if err != nil {
		t.Fatalf("GetBridgeOptions: %v", err)
	}
	if len(options.Candidates) != 1 || options.Candidates[0].Eligible {
		t.Fatalf("NetworkManager candidate = %#v", options.Candidates)
	}
	if reason := strings.Join(options.Candidates[0].Reasons, "; "); !strings.Contains(reason, "existing NetworkManager profile") {
		t.Fatalf("refusal reason = %q", reason)
	}
}

func TestCreateNetworkdBridgeRestoresFilesAfterApplyFailure(t *testing.T) {
	env, runner := bridgeTestEnvironment(t, []InterfaceProbe{{
		Name: "eth1", MAC: "00:11:22:33:44:66", Ethernet: true,
	}})
	memberPath := filepath.Join(env.NetworkdDir, "10-eth1.network")
	original := "[Match]\nName=eth1\n\n[Network]\nDHCP=no\n"
	mustWriteFile(t, memberPath, original)
	runner.fail("networkctl reconfigure br0", errors.New("reconfigure failed"))

	_, err := CreateBridge(context.Background(), env, BridgePlan{Name: "br0", Member: "eth1"})
	if err == nil || !strings.Contains(err.Error(), "reconfigure failed") {
		t.Fatalf("CreateBridge error = %v", err)
	}
	data, readErr := os.ReadFile(memberPath)
	if readErr != nil {
		t.Fatalf("read restored member file: %v", readErr)
	}
	if string(data) != original {
		t.Fatalf("restored member file = %q, want %q", data, original)
	}
	for _, path := range []string{
		filepath.Join(env.NetworkdDir, "90-linuxio-br0.netdev"),
		filepath.Join(env.NetworkdDir, "90-linuxio-br0.network"),
	} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("generated file %s still exists: %v", path, statErr)
		}
	}
	requireCalls(t, runner,
		"networkctl reload",
		"networkctl reconfigure br0",
		"networkctl reload",
		"networkctl reconfigure eth1",
	)
}

func TestCreateNetworkdBridgeDoesNotOverwriteUnrelatedMemberFile(t *testing.T) {
	env, runner := bridgeTestEnvironment(t, []InterfaceProbe{{
		Name: "eth1", MAC: "00:11:22:33:44:66", Ethernet: true,
	}})
	path := filepath.Join(env.NetworkdDir, "90-linuxio-br0-member.network")
	original := "[Match]\nName=eth9\n"
	mustWriteFile(t, path, original)

	_, err := CreateBridge(context.Background(), env, BridgePlan{Name: "br0", Member: "eth1"})
	if err == nil || !strings.Contains(err.Error(), "configuration already exists") {
		t.Fatalf("CreateBridge error = %v", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read member file: %v", readErr)
	}
	if string(data) != original {
		t.Fatalf("member file = %q, want %q", data, original)
	}
	requireCalls(t, runner)
}

func TestNetworkManagerProfilesDisableHostL3(t *testing.T) {
	if networkManagerCheckpointFlags != 0x06 {
		t.Fatalf("checkpoint flags = %#x, want delete-connections|disconnect-new-devices", networkManagerCheckpointFlags)
	}
	bridge := networkManagerBridgeSettings("br0", "11111111-1111-4111-8111-111111111111")
	port := networkManagerSlaveSettings("br0", "eth1", "22222222-2222-4222-8222-222222222222")

	if got := bridge["ipv4"]["method"].Value(); got != "disabled" {
		t.Fatalf("bridge IPv4 method = %v", got)
	}
	if got := bridge["ipv6"]["method"].Value(); got != "disabled" {
		t.Fatalf("bridge IPv6 method = %v", got)
	}
	if got := port["connection"]["master"].Value(); got != "br0" {
		t.Fatalf("port master = %v", got)
	}
	if _, ok := port["802-3-ethernet"]; !ok {
		t.Fatal("port profile is missing its Ethernet setting")
	}
}

func TestNetplanBridgeFailurePreservesOperationAndCleanupErrors(t *testing.T) {
	operationErr := errors.New("apply failed")
	cleanupErr := errors.New("delete bridge failed")
	env := Environment{
		RemoveBridge: func(string) error { return cleanupErr },
	}

	err := netplanBridgeFailure(context.Background(), operationErr, env, "", "br0", true)
	if !errors.Is(err, operationErr) {
		t.Fatalf("netplanBridgeFailure = %v, want operation error identity", err)
	}
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("netplanBridgeFailure = %v, want cleanup error identity", err)
	}
}

func TestValidateBridgeName(t *testing.T) {
	for _, name := range []string{"", ".", "..", "br:test", "br test", "br/test", "1234567890123456"} {
		if err := validateBridgeName(name); err == nil {
			t.Errorf("validateBridgeName(%q) succeeded", name)
		}
	}
	for _, name := range []string{"br0", "br.test", "linuxio-br1"} {
		if err := validateBridgeName(name); err != nil {
			t.Errorf("validateBridgeName(%q): %v", name, err)
		}
	}
}
