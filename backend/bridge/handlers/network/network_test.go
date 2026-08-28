package network

import (
	"context"
	"encoding/json"
	stdnet "net"
	"strings"
	"testing"

	"github.com/shirou/gopsutil/v4/net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	networkbackend "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/network/internal/network"
	bridgeruntime "github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

type fakeBridgeHandoffService struct {
	uid       uint32
	operation string
}

func (f *fakeBridgeHandoffService) Start(_ context.Context, uid uint32, req apischema.NetworkBridgeHandoffRequest) (apischema.NetworkBridgeHandoffStatus, error) {
	f.uid = uid
	f.operation = req.OperationID
	return apischema.NetworkBridgeHandoffStatus{OperationID: req.OperationID, State: apischema.NetworkBridgeHandoffApplying}, nil
}

func (f *fakeBridgeHandoffService) Status(_ context.Context, uid uint32, operationID string) (apischema.NetworkBridgeHandoffStatus, error) {
	f.uid = uid
	f.operation = operationID
	return apischema.NetworkBridgeHandoffStatus{OperationID: operationID, State: apischema.NetworkBridgeHandoffAwaitingConfirmation}, nil
}

func (f *fakeBridgeHandoffService) Confirm(_ context.Context, uid uint32, operationID string) (apischema.NetworkBridgeHandoffStatus, error) {
	f.uid = uid
	f.operation = operationID
	return apischema.NetworkBridgeHandoffStatus{OperationID: operationID, State: apischema.NetworkBridgeHandoffConfirmed}, nil
}

func (f *fakeBridgeHandoffService) Revert(_ context.Context, uid uint32, operationID string) (apischema.NetworkBridgeHandoffStatus, error) {
	f.uid = uid
	f.operation = operationID
	return apischema.NetworkBridgeHandoffStatus{OperationID: operationID, State: apischema.NetworkBridgeHandoffReverted}, nil
}

func TestValidateHandoffStartRequiresConsoleAcknowledgement(t *testing.T) {
	req := apischema.NetworkBridgeHandoffRequest{
		OperationID: "00000000-0000-4000-8000-000000000001",
		Name:        "br-eth0",
		Member:      "eth0",
	}
	if err := validateHandoffStartRequest(req); err == nil {
		t.Fatal("expected the console acknowledgement to be required")
	}
	req.ConsoleAcknowledged = true
	if err := validateHandoffStartRequest(req); err != nil {
		t.Fatalf("valid handoff request rejected: %v", err)
	}
}

func TestNetworkHandlersPassSessionUIDToHandoffAdapter(t *testing.T) {
	fake := &fakeBridgeHandoffService{}
	h := networkHandlers{
		rt:      bridgeruntime.Runtime{Session: &session.Session{User: session.User{UID: 1007}}},
		handoff: validatingBridgeHandoffService{inner: fake},
	}
	req := apischema.NetworkBridgeHandoffRequest{
		OperationID:         "00000000-0000-4000-8000-000000000002",
		Name:                "br-eth0",
		Member:              "eth0",
		ConsoleAcknowledged: true,
	}
	if _, err := h.handleStartBridgeHandoff(context.Background(), req); err != nil {
		t.Fatalf("start handoff: %v", err)
	}
	if fake.uid != 1007 || fake.operation != req.OperationID {
		t.Fatalf("adapter received uid=%d operation=%q", fake.uid, fake.operation)
	}
}

func TestMergeConfiguredStatePrefersManualConfiguredValues(t *testing.T) {
	liveMethod := "unknown"
	info := apischema.NetworkInterface{
		Name:       "eth0",
		IPv4:       []string{"192.168.1.5/24"},
		DNS:        []string{"192.168.1.1"},
		Gateway:    "192.168.1.1",
		IPv4Method: &liveMethod,
	}
	cfg := networkbackend.InterfaceConfig{
		Backend:       "netplan",
		IPv4Method:    "manual",
		IPv4Addresses: []string{"10.0.0.20/24"},
		DNS:           []string{"1.1.1.1", "8.8.8.8"},
		Gateway:       "10.0.0.1",
	}

	mergeConfiguredState(&info, cfg)

	if got := info.IPv4[0]; got != "10.0.0.20/24" {
		t.Fatalf("expected configured IPv4 address, got %s", got)
	}
	if got := info.Gateway; got != "10.0.0.1" {
		t.Fatalf("expected configured gateway, got %s", got)
	}
	if got := info.DNS[0]; got != "1.1.1.1" {
		t.Fatalf("expected configured DNS, got %v", info.DNS)
	}
	if info.IPv4Method == nil || *info.IPv4Method != "manual" {
		t.Fatalf("expected manual IPv4 method, got %v", info.IPv4Method)
	}
	if info.ConfigBackend != "netplan" {
		t.Fatalf("expected detected config backend, got %q", info.ConfigBackend)
	}
}

func TestMergeConfiguredStateBackfillsEmptyLiveValues(t *testing.T) {
	info := apischema.NetworkInterface{Name: "eth1"}
	cfg := networkbackend.InterfaceConfig{
		Backend:       "ifcfg",
		IPv4Method:    "manual",
		IPv4Addresses: []string{"172.16.10.5/24"},
		DNS:           []string{"9.9.9.9"},
		Gateway:       "172.16.10.1",
	}

	mergeConfiguredState(&info, cfg)

	if len(info.IPv4) != 1 || info.IPv4[0] != "172.16.10.5/24" {
		t.Fatalf("expected configured IPv4 addresses, got %v", info.IPv4)
	}
	if len(info.DNS) != 1 || info.DNS[0] != "9.9.9.9" {
		t.Fatalf("expected configured DNS, got %v", info.DNS)
	}
	if info.Gateway != "172.16.10.1" {
		t.Fatalf("expected configured gateway, got %s", info.Gateway)
	}
}

func TestMergeConfiguredStateKeepsLiveMethodWhenBackendHasNone(t *testing.T) {
	liveMethod := "unknown"
	info := apischema.NetworkInterface{Name: "eth2", IPv4Method: &liveMethod}

	mergeConfiguredState(&info, networkbackend.InterfaceConfig{Backend: "ifupdown"})

	if info.IPv4Method == nil || *info.IPv4Method != "unknown" {
		t.Fatalf("expected the live method to survive, got %v", info.IPv4Method)
	}
}

func TestMapBridgeOptionsPreservesCandidatesAndHostWarnings(t *testing.T) {
	options := networkbackend.BridgeOptions{
		Candidates: []networkbackend.BridgeCandidate{{
			Name:            "enp2s0",
			MAC:             "52:54:00:00:00:01",
			Backend:         "systemd-networkd",
			Eligible:        false,
			Reasons:         []string{"interface has an address"},
			HandoffEligible: true,
			HandoffReasons:  []string{"handoff warning"},
		}},
		Warnings: []string{"firewall inspection unavailable"},
	}

	got := mapBridgeOptions(options)
	if len(got.Candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(got.Candidates))
	}
	candidate := got.Candidates[0]
	if candidate.Name != "enp2s0" || candidate.MAC != "52:54:00:00:00:01" || candidate.Backend != "systemd-networkd" {
		t.Fatalf("candidate identity = %+v", candidate)
	}
	if candidate.Eligible || len(candidate.Reasons) != 1 {
		t.Fatalf("candidate safety fields = %+v", candidate)
	}
	if !candidate.HandoffEligible || candidate.HandoffReasons == nil {
		t.Fatalf("candidate handoff fields = %+v", candidate)
	}
	if len(got.Warnings) != 1 || got.Warnings[0] != "firewall inspection unavailable" {
		t.Fatalf("options warnings = %v", got.Warnings)
	}
}

func TestNetworkInterfaceCountersReportTheSnapshotRaw(t *testing.T) {
	snapshots := map[string]net.IOCountersStat{
		"eth0": {
			Name:        "eth0",
			BytesRecv:   4096,
			BytesSent:   2048,
			PacketsRecv: 40,
			PacketsSent: 20,
			Errin:       3,
			Errout:      2,
			Dropin:      1,
			Dropout:     5,
		},
	}

	counters := networkInterfaceCounters("eth0", snapshots)

	expected := apischema.NetworkInterfaceCounters{
		RXBytes:   4096,
		RXDropped: 1,
		RXErrors:  3,
		RXPackets: 40,
		TXBytes:   2048,
		TXDropped: 5,
		TXErrors:  2,
		TXPackets: 20,
	}
	if counters != expected {
		t.Fatalf("expected %+v, got %+v", expected, counters)
	}
	if zero := networkInterfaceCounters("eth1", snapshots); zero != (apischema.NetworkInterfaceCounters{}) {
		t.Fatalf("expected zero counters without a snapshot, got %+v", zero)
	}
}

func withInstalledUnits(t *testing.T, units ...string) {
	t.Helper()
	installed := make(map[string]struct{}, len(units))
	for _, unit := range units {
		installed[unit] = struct{}{}
	}
	previous := systemdUnitInstalled
	systemdUnitInstalled = func(unit string) bool {
		_, ok := installed[unit]
		return ok
	}
	networkLogUnitCache = map[string]string{}
	t.Cleanup(func() {
		systemdUnitInstalled = previous
		networkLogUnitCache = map[string]string{}
	})
}

func TestResolveNetworkLogUnitPrefersTheBackendsOwnUnit(t *testing.T) {
	withInstalledUnits(t, "NetworkManager.service", "systemd-networkd.service")

	if unit := resolveNetworkLogUnit("systemd-networkd"); unit != "systemd-networkd.service" {
		t.Fatalf("expected the networkd unit, got %q", unit)
	}
	if unit := resolveNetworkLogUnit("nmconnection"); unit != "NetworkManager.service" {
		t.Fatalf("expected the NetworkManager unit, got %q", unit)
	}
}

func TestResolveNetworkLogUnitFallsBackWhenTheBackendUnitIsMissing(t *testing.T) {
	withInstalledUnits(t, "NetworkManager.service")

	if unit := resolveNetworkLogUnit("ifupdown"); unit != "NetworkManager.service" {
		t.Fatalf("expected the fallback unit, got %q", unit)
	}
	if unit := resolveNetworkLogUnit(""); unit != "NetworkManager.service" {
		t.Fatalf("expected the fallback unit for an unknown backend, got %q", unit)
	}
}

func TestResolveNetworkLogUnitIsEmptyWhenNothingIsInstalled(t *testing.T) {
	withInstalledUnits(t)

	if unit := resolveNetworkLogUnit("netplan"); unit != "" {
		t.Fatalf("expected no unit, got %q", unit)
	}
}

func TestResolveNetworkLogUnitMemoizesPerBackend(t *testing.T) {
	withInstalledUnits(t, "networking.service")
	lookups := 0
	previous := systemdUnitInstalled
	systemdUnitInstalled = func(unit string) bool {
		lookups++
		return previous(unit)
	}

	first := resolveNetworkLogUnit("ifupdown")
	lookupsAfterFirst := lookups
	second := resolveNetworkLogUnit("ifupdown")

	if first != "networking.service" || second != first {
		t.Fatalf("expected a stable unit, got %q then %q", first, second)
	}
	if lookups != lookupsAfterFirst {
		t.Fatalf("expected the cached answer, saw %d extra lookups", lookups-lookupsAfterFirst)
	}
}

func TestLiveInterfaceInfoAlwaysSerialisesArrays(t *testing.T) {
	// A disconnected interface has no addresses and the host may have no
	// nameservers; both fields are typed as arrays on the wire, so null would
	// crash any consumer that trusts the contract.
	info := liveInterfaceInfo(
		stdnet.Interface{Name: "eth9"},
		nil,
		"",
		map[string]net.IOCountersStat{},
		1,
	)

	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, field := range []string{`"ipv4":[]`, `"dns":[]`} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("expected %s in %s", field, encoded)
		}
	}
}
