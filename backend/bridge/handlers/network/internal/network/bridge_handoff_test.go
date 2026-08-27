package network

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	godbus "github.com/godbus/dbus/v5"
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
		"connection":     {"type": godbus.MakeVariant("802-3-ethernet")},
		"802-3-ethernet": {"mtu": godbus.MakeVariant(uint32(1400))},
		"ipv4":           {"method": godbus.MakeVariant("auto")},
		"ipv6":           {"method": godbus.MakeVariant("auto")},
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
	source["802-1x"] = map[string]godbus.Variant{}
	if err := validateNetworkManagerHandoffSettings(source); err == nil || !strings.Contains(err.Error(), "802.1X") {
		t.Fatalf("802.1X error = %v", err)
	}
}
