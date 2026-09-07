package network

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient/testdbus"
)

type netplanConfigRecorder struct{ methods []string }

func (r *netplanConfigRecorder) CallWithContext(_ context.Context, method string, _ godbus.Flags, _ ...any) *godbus.Call {
	r.methods = append(r.methods, method)
	return &godbus.Call{Body: []any{true}}
}

func TestHandoffCandidateAllowsNetplanManagementNIC(t *testing.T) {
	env, _ := bridgeTestEnvironment(t, []InterfaceProbe{{
		Name: "eth0", MAC: "00:11:22:33:44:55", Ethernet: true,
		Addresses: []string{"192.0.2.10/24"}, DefaultRoute: true,
	}})
	mustWriteFile(t, env.NetplanDir+"/10-eth0.yaml", `
network:
  version: 2
  renderer: networkd
  ethernets:
    eth0:
      dhcp4: true
`)

	options, err := GetBridgeOptions(context.Background(), env)
	if err != nil {
		t.Fatalf("GetBridgeOptions: %v", err)
	}
	candidate := options.Candidates[0]
	if candidate.Eligible || !candidate.HandoffEligible || candidate.Backend != bridgeBackendNetplan {
		t.Fatalf("candidate = %#v", candidate)
	}
}

func TestHandoffCandidateRefusesBareNetworkd(t *testing.T) {
	env, _ := bridgeTestEnvironment(t, []InterfaceProbe{{
		Name: "eth0", MAC: "00:11:22:33:44:55", Ethernet: true,
		Addresses: []string{"192.0.2.10/24"}, DefaultRoute: true,
	}})
	mustWriteFile(t, env.NetworkdDir+"/10-eth0.network", "[Match]\nName=eth0\n\n[Network]\nDHCP=yes\n")

	options, err := GetBridgeOptions(context.Background(), env)
	if err != nil {
		t.Fatalf("GetBridgeOptions: %v", err)
	}
	candidate := options.Candidates[0]
	if candidate.HandoffEligible || !strings.Contains(strings.Join(candidate.HandoffReasons, "; "), "define the bridge in networkd configuration") {
		t.Fatalf("candidate = %#v", candidate)
	}
}

func TestPrepareBridgeHandoffRequiresConsoleAcknowledgement(t *testing.T) {
	env, _ := bridgeTestEnvironment(t, nil)
	_, err := PrepareBridgeHandoff(context.Background(), env, BridgeHandoffPlan{Name: "br0", Member: "eth0"})
	if err == nil || !strings.Contains(err.Error(), "console acknowledgement") {
		t.Fatalf("PrepareBridgeHandoff error = %v", err)
	}
}

func TestNetplanHandoffDeltaMovesL3AndPinsMAC(t *testing.T) {
	env, _, _ := testEnv(t)
	mustWriteFile(t, env.NetplanDir+"/10-eth0.yaml", `
network:
  version: 2
  renderer: networkd
  ethernets:
    eth0:
      dhcp4: true
      addresses: [192.0.2.10/24]
      routes:
        - to: default
          via: 192.0.2.1
      nameservers:
        addresses: [192.0.2.53]
      mtu: 1400
`)
	state := BridgeHandoffState{
		Plan:      BridgeHandoffPlan{Name: "br0", Member: "eth0", ConsoleAcknowledged: true},
		MemberMAC: "00:11:22:33:44:55",
	}
	memberDelta, bridgeDelta, err := netplanHandoffDeltas(env, &state)
	if err != nil {
		t.Fatalf("netplanHandoffDeltas: %v", err)
	}
	var member, bridge map[string]any
	if err := json.Unmarshal(memberDelta, &member); err != nil {
		t.Fatalf("decode member: %v", err)
	}
	if err := json.Unmarshal(bridgeDelta, &bridge); err != nil {
		t.Fatalf("decode bridge: %v", err)
	}
	if member["addresses"] != nil || member["routes"] != nil || member["dhcp4"] != false {
		t.Fatalf("member delta = %#v", member)
	}
	if bridge["macaddress"] != state.MemberMAC || bridge["addresses"] == nil || bridge["routes"] == nil || bridge["mtu"] != float64(1400) {
		t.Fatalf("bridge delta = %#v", bridge)
	}
}

func TestNetplanHandoffDeltaPreservesEffectiveRenderer(t *testing.T) {
	for name, content := range map[string]string{
		"interface": `
network:
  version: 2
  renderer: networkd
  ethernets:
    eth0:
      renderer: NetworkManager
      dhcp4: true
      accept-ra: false
`,
		"group": `
network:
  version: 2
  renderer: networkd
  ethernets:
    renderer: NetworkManager
    eth0:
      dhcp4: true
      accept-ra: false
`,
	} {
		t.Run(name, func(t *testing.T) {
			env, _, _ := testEnv(t)
			mustWriteFile(t, env.NetplanDir+"/10-eth0.yaml", content)
			state := BridgeHandoffState{
				Plan:      BridgeHandoffPlan{Name: "br0", Member: "eth0", ConsoleAcknowledged: true},
				MemberMAC: "00:11:22:33:44:55",
			}
			_, bridgeDelta, err := netplanHandoffDeltas(env, &state)
			if err != nil {
				t.Fatalf("netplanHandoffDeltas: %v", err)
			}
			var bridge map[string]any
			if err := json.Unmarshal(bridgeDelta, &bridge); err != nil {
				t.Fatalf("decode bridge: %v", err)
			}
			if bridge["renderer"] != "NetworkManager" {
				t.Fatalf("bridge renderer = %#v, want NetworkManager", bridge["renderer"])
			}
		})
	}
}

func TestNetplanHandoffDeltaPinsDefaultNetworkdRenderer(t *testing.T) {
	env, _, _ := testEnv(t)
	mustWriteFile(t, env.NetplanDir+"/10-eth0.yaml", `
network:
  version: 2
  ethernets:
    eth0:
      dhcp4: true
  bridges:
    br0:
      renderer: NetworkManager
`)
	state := BridgeHandoffState{
		Plan:      BridgeHandoffPlan{Name: "br0", Member: "eth0", ConsoleAcknowledged: true},
		MemberMAC: "00:11:22:33:44:55",
	}
	_, bridgeDelta, err := netplanHandoffDeltas(env, &state)
	if err != nil {
		t.Fatalf("netplanHandoffDeltas: %v", err)
	}
	var bridge map[string]any
	if err := json.Unmarshal(bridgeDelta, &bridge); err != nil {
		t.Fatalf("decode bridge: %v", err)
	}
	if bridge["renderer"] != "networkd" {
		t.Fatalf("bridge renderer = %#v, want networkd", bridge["renderer"])
	}
}

func TestNetplanHandoffRefusesStablePrivacyIPv6(t *testing.T) {
	env, _, _ := testEnv(t)
	mustWriteFile(t, env.NetplanDir+"/10-eth0.yaml", `
network:
  version: 2
  renderer: networkd
  ethernets:
    eth0:
      accept-ra: true
      ipv6-address-generation: stable-privacy
`)
	state := BridgeHandoffState{
		Plan:      BridgeHandoffPlan{Name: "br0", Member: "eth0", ConsoleAcknowledged: true},
		MemberMAC: "00:11:22:33:44:55",
	}
	_, _, err := netplanHandoffDeltas(env, &state)
	if err == nil || !strings.Contains(err.Error(), "stable-privacy") {
		t.Fatalf("netplanHandoffDeltas error = %v", err)
	}
}

func TestNetplanHandoffRefusesNetworkManagerDynamicIPv6(t *testing.T) {
	env, _, _ := testEnv(t)
	mustWriteFile(t, env.NetplanDir+"/10-eth0.yaml", `
network:
  renderer: NetworkManager
  ethernets:
    eth0:
      accept-ra: true
      ipv6-address-generation: eui64
`)
	state := BridgeHandoffState{
		Plan:      BridgeHandoffPlan{Name: "br0", Member: "eth0", ConsoleAcknowledged: true},
		MemberMAC: "00:11:22:33:44:55",
	}
	_, _, err := netplanHandoffDeltas(env, &state)
	if err == nil || !strings.Contains(err.Error(), "rendered by NetworkManager") {
		t.Fatalf("netplanHandoffDeltas error = %v", err)
	}
}

func TestConfigureNetplanHandoffLeavesTryPending(t *testing.T) {
	config := &netplanConfigRecorder{}
	state := &BridgeHandoffState{Plan: BridgeHandoffPlan{Name: "br0", Member: "eth0"}}
	if err := configureNetplanHandoff(context.Background(), config, state, []byte(`{}`), []byte(`{}`)); err != nil {
		t.Fatalf("configureNetplanHandoff: %v", err)
	}
	want := []string{netplanConfigIface + ".Set", netplanConfigIface + ".Set", netplanConfigIface + ".Try"}
	if strings.Join(config.methods, ",") != strings.Join(want, ",") {
		t.Fatalf("methods = %v, want %v", config.methods, want)
	}
}

func TestBridgeHandoffUsesNativeTransactionHandle(t *testing.T) {
	oldStart := startNetworkManagerHandoffNative
	oldConfirm := confirmNetworkManagerHandoffNative
	oldRevert := revertNetworkManagerHandoffNative
	t.Cleanup(func() {
		startNetworkManagerHandoffNative = oldStart
		confirmNetworkManagerHandoffNative = oldConfirm
		revertNetworkManagerHandoffNative = oldRevert
	})
	wantPath := godbus.ObjectPath("/org/freedesktop/NetworkManager/Checkpoint/1")
	startNetworkManagerHandoffNative = func(context.Context, BridgeHandoffPlan, string) (godbus.ObjectPath, error) { return wantPath, nil }
	var confirmed, reverted godbus.ObjectPath
	confirmNetworkManagerHandoffNative = func(_ context.Context, path godbus.ObjectPath) error { confirmed = path; return nil }
	revertNetworkManagerHandoffNative = func(_ context.Context, path godbus.ObjectPath) error { reverted = path; return nil }
	env := Environment{VerifyBridgeHandoff: func(context.Context, *BridgeHandoffState) (bool, error) { return true, nil }}
	state := BridgeHandoffState{
		Plan:    BridgeHandoffPlan{Name: "br0", Member: "eth0", ConsoleAcknowledged: true},
		Backend: bridgeBackendNetworkManager, MemberMAC: "00:11:22:33:44:55",
	}
	if err := ApplyBridgeHandoff(context.Background(), env, &state); err != nil {
		t.Fatalf("ApplyBridgeHandoff: %v", err)
	}
	if state.Handle != string(wantPath) {
		t.Fatalf("handle = %q", state.Handle)
	}
	if err := ConfirmBridgeHandoff(context.Background(), env, &state); err != nil {
		t.Fatalf("ConfirmBridgeHandoff: %v", err)
	}
	if err := RevertBridgeHandoff(context.Background(), &state); err != nil {
		t.Fatalf("RevertBridgeHandoff: %v", err)
	}
	if confirmed != wantPath || reverted != wantPath {
		t.Fatalf("confirmed %q, reverted %q", confirmed, reverted)
	}
}

func TestNetworkManagerHandoffSettingsCopyL3AndRefuse8021X(t *testing.T) {
	source := map[string]map[string]godbus.Variant{
		"connection":     {"type": godbus.MakeVariant("802-3-ethernet"), "uuid": godbus.MakeVariant("11111111-1111-4111-8111-111111111111"), "zone": godbus.MakeVariant("Home"), "stable-id": godbus.MakeVariant("host-stable")},
		"802-3-ethernet": {"mtu": godbus.MakeVariant(uint32(1400))},
		"ipv4":           {"method": godbus.MakeVariant("auto")},
		"ipv6":           {"method": godbus.MakeVariant("auto"), "addr-gen-mode": godbus.MakeVariant(int32(0)), "ip6-privacy": godbus.MakeVariant(int32(0)), "dhcp-duid": godbus.MakeVariant("ll"), "dhcp-iaid": godbus.MakeVariant("mac")},
	}
	if err := validateNetworkManagerHandoffSettings(source); err != nil {
		t.Fatalf("validate settings: %v", err)
	}
	bridge, port := networkManagerHandoffSettings(BridgeHandoffPlan{Name: "br0", Member: "eth0"}, "00:11:22:33:44:55", source)
	if variantString(bridge["ipv4"]["method"]) != "auto" || variantString(bridge["802-3-ethernet"]["cloned-mac-address"]) != "00:11:22:33:44:55" {
		t.Fatalf("bridge settings = %#v", bridge)
	}
	if port["802-3-ethernet"]["mtu"].Value() != uint32(1400) || variantString(port["ipv4"]["method"]) != "disabled" {
		t.Fatalf("port settings = %#v", port)
	}
	if variantString(bridge["connection"]["zone"]) != "Home" || variantString(bridge["connection"]["stable-id"]) != "host-stable" {
		t.Fatalf("bridge connection context = %#v", bridge["connection"])
	}
	source["802-1x"] = map[string]godbus.Variant{}
	if err := validateNetworkManagerHandoffSettings(source); err == nil || !strings.Contains(err.Error(), "802.1X") {
		t.Fatalf("802.1X error = %v", err)
	}
}

func TestNetworkManagerHandoffRefusesUnsupportedSettingsBeforeMutation(t *testing.T) {
	source := map[string]map[string]godbus.Variant{
		"connection":     {"type": godbus.MakeVariant("802-3-ethernet")},
		"802-3-ethernet": {},
		"ipv4":           {"method": godbus.MakeVariant("auto")},
		"ipv6":           {"method": godbus.MakeVariant("disabled")},
		"vpn":            {},
	}
	err := validateNetworkManagerHandoffSettings(source)
	if err == nil || !strings.Contains(err.Error(), `setting "vpn"`) {
		t.Fatalf("validation error = %v", err)
	}
}

func TestNetworkManagerHandoffRefusesUnpreservableIPv4Identity(t *testing.T) {
	for name, testCase := range map[string]struct {
		connection map[string]godbus.Variant
		ipv4       map[string]godbus.Variant
		want       string
	}{
		"stable client id without stable id": {
			connection: map[string]godbus.Variant{"type": godbus.MakeVariant("802-3-ethernet")},
			ipv4:       map[string]godbus.Variant{"method": godbus.MakeVariant("auto"), "dhcp-client-id": godbus.MakeVariant("stable")},
			want:       "explicit connection.stable-id",
		},
		"stable client id uses permanent MAC": {
			connection: map[string]godbus.Variant{"type": godbus.MakeVariant("802-3-ethernet"), "stable-id": godbus.MakeVariant("host-${MAC}")},
			ipv4:       map[string]godbus.Variant{"method": godbus.MakeVariant("auto"), "dhcp-client-id": godbus.MakeVariant("stable")},
			want:       "source interface or connection",
		},
		"duid without IAID": {
			connection: map[string]godbus.Variant{"type": godbus.MakeVariant("802-3-ethernet")},
			ipv4:       map[string]godbus.Variant{"method": godbus.MakeVariant("auto"), "dhcp-client-id": godbus.MakeVariant("duid")},
			want:       "implicit interface-name IAID",
		},
		"ipv6 DUID with empty IAID": {
			connection: map[string]godbus.Variant{"type": godbus.MakeVariant("802-3-ethernet")},
			ipv4:       map[string]godbus.Variant{"method": godbus.MakeVariant("auto"), "dhcp-client-id": godbus.MakeVariant("ipv6-duid"), "dhcp-iaid": godbus.MakeVariant("")},
			want:       "implicit interface-name IAID",
		},
		"interface IAID": {
			connection: map[string]godbus.Variant{"type": godbus.MakeVariant("802-3-ethernet")},
			ipv4:       map[string]godbus.Variant{"method": godbus.MakeVariant("auto"), "dhcp-client-id": godbus.MakeVariant("duid"), "dhcp-iaid": godbus.MakeVariant("ifname")},
			want:       "source interface",
		},
	} {
		t.Run(name, func(t *testing.T) {
			source := map[string]map[string]godbus.Variant{
				"connection": testCase.connection, "802-3-ethernet": {}, "ipv4": testCase.ipv4, "ipv6": {"method": godbus.MakeVariant("disabled")},
			}
			if err := validateNetworkManagerHandoffSettings(source); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("validation error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestNetworkManagerHandoffRefusesUnpreservableIPv6Identity(t *testing.T) {
	for name, testCase := range map[string]struct {
		ipv6 map[string]godbus.Variant
		want string
	}{
		"stable privacy": {
			ipv6: map[string]godbus.Variant{
				"method": godbus.MakeVariant("auto"), "addr-gen-mode": godbus.MakeVariant(int32(1)), "ip6-privacy": godbus.MakeVariant(int32(0)),
			},
			want: "interface-dependent address generation",
		},
		"privacy extensions": {
			ipv6: map[string]godbus.Variant{
				"method": godbus.MakeVariant("auto"), "addr-gen-mode": godbus.MakeVariant(int32(0)), "ip6-privacy": godbus.MakeVariant(int32(2)),
			},
			want: "privacy extensions",
		},
		"interface stable id": {
			ipv6: map[string]godbus.Variant{
				"method": godbus.MakeVariant("auto"), "addr-gen-mode": godbus.MakeVariant(int32(0)), "ip6-privacy": godbus.MakeVariant(int32(0)), "dhcp-duid": godbus.MakeVariant("ll"), "dhcp-iaid": godbus.MakeVariant("mac"),
			},
			want: "source interface or connection",
		},
		"lease DUID": {
			ipv6: map[string]godbus.Variant{
				"method": godbus.MakeVariant("auto"), "addr-gen-mode": godbus.MakeVariant(int32(0)), "ip6-privacy": godbus.MakeVariant(int32(0)), "dhcp-duid": godbus.MakeVariant("lease"), "dhcp-iaid": godbus.MakeVariant("mac"),
			},
			want: "lease identity",
		},
		"stable DUID without stable id": {
			ipv6: map[string]godbus.Variant{
				"method": godbus.MakeVariant("auto"), "addr-gen-mode": godbus.MakeVariant(int32(0)), "ip6-privacy": godbus.MakeVariant(int32(0)), "dhcp-duid": godbus.MakeVariant("stable-ll"), "dhcp-iaid": godbus.MakeVariant("mac"),
			},
			want: "explicit connection.stable-id",
		},
		"implicit DHCP identity": {
			ipv6: map[string]godbus.Variant{
				"method": godbus.MakeVariant("auto"), "addr-gen-mode": godbus.MakeVariant(int32(0)), "ip6-privacy": godbus.MakeVariant(int32(0)),
			},
			want: "implicit DHCPv6",
		},
	} {
		t.Run(name, func(t *testing.T) {
			connection := map[string]godbus.Variant{"type": godbus.MakeVariant("802-3-ethernet")}
			if name == "interface stable id" {
				connection["stable-id"] = godbus.MakeVariant("host-${DEVICE}")
			}
			source := map[string]map[string]godbus.Variant{
				"connection": connection, "802-3-ethernet": {}, "ipv4": {"method": godbus.MakeVariant("auto")}, "ipv6": testCase.ipv6,
			}
			if err := validateNetworkManagerHandoffSettings(source); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("validation error = %v, want %q", err, testCase.want)
			}
		})
	}
}

type networkManagerRollbackStub struct {
	results map[string]uint32
}

func (s *networkManagerRollbackStub) CheckpointRollback(godbus.ObjectPath) (map[string]uint32, *godbus.Error) {
	return s.results, nil
}

func (s *networkManagerRollbackStub) CheckpointDestroy(godbus.ObjectPath) *godbus.Error {
	return nil
}

func TestNetworkManagerRollbackChecksPerDeviceResults(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	conn := bus.OwnName(t, networkManagerBusName)
	stub := &networkManagerRollbackStub{results: map[string]uint32{"/org/freedesktop/NetworkManager/Devices/1": 3}}
	if err := conn.Export(stub, godbus.ObjectPath(networkManagerPath), networkManagerIface); err != nil {
		t.Fatalf("export NetworkManager stub: %v", err)
	}
	err := (networkManagerMutation{checkpoint: godbus.ObjectPath("/org/freedesktop/NetworkManager/Checkpoint/1")}).rollback(context.Background())
	if err == nil || !strings.Contains(err.Error(), "result 3") {
		t.Fatalf("rollback error = %v", err)
	}
}

func TestNetworkManagerRollbackAcceptsSuccessfulDeviceResults(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	conn := bus.OwnName(t, networkManagerBusName)
	stub := &networkManagerRollbackStub{results: map[string]uint32{"/org/freedesktop/NetworkManager/Devices/1": 0}}
	if err := conn.Export(stub, godbus.ObjectPath(networkManagerPath), networkManagerIface); err != nil {
		t.Fatalf("export NetworkManager stub: %v", err)
	}
	if err := (networkManagerMutation{checkpoint: godbus.ObjectPath("/org/freedesktop/NetworkManager/Checkpoint/1")}).rollback(context.Background()); err != nil {
		t.Fatalf("rollback error = %v", err)
	}
}
