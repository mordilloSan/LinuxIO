package network

import (
	"encoding/json"
	stdnet "net"
	"strings"
	"testing"

	"github.com/shirou/gopsutil/v4/net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	networkbackend "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/network/internal/network"
)

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
