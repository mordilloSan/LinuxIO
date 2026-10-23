package network

import (
	"testing"
	"time"

	psutilNet "github.com/shirou/gopsutil/v4/net"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/deltatracker"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

func TestIsValidNic(t *testing.T) {
	tests := []struct {
		name          string
		nicName       string
		config        *NicConfig
		expectedValid bool
	}{
		{
			name:    "Whitelist - NIC in list",
			nicName: "eth0",
			config: &NicConfig{
				nics:        map[string]struct{}{"eth0": {}},
				isBlacklist: false,
			},
			expectedValid: true,
		},
		{
			name:    "Whitelist - NIC not in list",
			nicName: "wlan0",
			config: &NicConfig{
				nics:        map[string]struct{}{"eth0": {}},
				isBlacklist: false,
			},
			expectedValid: false,
		},
		{
			name:    "Blacklist - NIC in list",
			nicName: "eth0",
			config: &NicConfig{
				nics:        map[string]struct{}{"eth0": {}},
				isBlacklist: true,
			},
			expectedValid: false,
		},
		{
			name:    "Blacklist - NIC not in list",
			nicName: "wlan0",
			config: &NicConfig{
				nics:        map[string]struct{}{"eth0": {}},
				isBlacklist: true,
			},
			expectedValid: true,
		},
		{
			name:    "Whitelist with wildcard - matching pattern",
			nicName: "eth1",
			config: &NicConfig{
				nics:         map[string]struct{}{"eth*": {}},
				isBlacklist:  false,
				hasWildcards: true,
			},
			expectedValid: true,
		},
		{
			name:    "Whitelist with wildcard - non-matching pattern",
			nicName: "wlan0",
			config: &NicConfig{
				nics:         map[string]struct{}{"eth*": {}},
				isBlacklist:  false,
				hasWildcards: true,
			},
			expectedValid: false,
		},
		{
			name:    "Blacklist with wildcard - matching pattern",
			nicName: "eth1",
			config: &NicConfig{
				nics:         map[string]struct{}{"eth*": {}},
				isBlacklist:  true,
				hasWildcards: true,
			},
			expectedValid: false,
		},
		{
			name:    "Blacklist with wildcard - non-matching pattern",
			nicName: "wlan0",
			config: &NicConfig{
				nics:         map[string]struct{}{"eth*": {}},
				isBlacklist:  true,
				hasWildcards: true,
			},
			expectedValid: true,
		},
		{
			name:    "Empty whitelist config - no NICs allowed",
			nicName: "eth0",
			config: &NicConfig{
				nics:        map[string]struct{}{},
				isBlacklist: false,
			},
			expectedValid: false,
		},
		{
			name:    "Empty blacklist config - all NICs allowed",
			nicName: "eth0",
			config: &NicConfig{
				nics:        map[string]struct{}{},
				isBlacklist: true,
			},
			expectedValid: true,
		},
		{
			name:    "Multiple patterns - exact match",
			nicName: "eth0",
			config: &NicConfig{
				nics:        map[string]struct{}{"eth0": {}, "wlan*": {}},
				isBlacklist: false,
			},
			expectedValid: true,
		},
		{
			name:    "Multiple patterns - wildcard match",
			nicName: "wlan1",
			config: &NicConfig{
				nics:         map[string]struct{}{"eth0": {}, "wlan*": {}},
				isBlacklist:  false,
				hasWildcards: true,
			},
			expectedValid: true,
		},
		{
			name:    "Multiple patterns - no match",
			nicName: "bond0",
			config: &NicConfig{
				nics:         map[string]struct{}{"eth0": {}, "wlan*": {}},
				isBlacklist:  false,
				hasWildcards: true,
			},
			expectedValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidNic(tt.nicName, tt.config)
			assert.Equal(t, tt.expectedValid, result)
		})
	}
}

func TestNewNicConfig(t *testing.T) {
	tests := []struct {
		name        string
		nicsEnvVal  string
		expectedCfg *NicConfig
	}{
		{
			name:       "Empty string",
			nicsEnvVal: "",
			expectedCfg: &NicConfig{
				nics:         map[string]struct{}{},
				isBlacklist:  false,
				hasWildcards: false,
			},
		},
		{
			name:       "Single NIC whitelist",
			nicsEnvVal: "eth0",
			expectedCfg: &NicConfig{
				nics:         map[string]struct{}{"eth0": {}},
				isBlacklist:  false,
				hasWildcards: false,
			},
		},
		{
			name:       "Multiple NICs whitelist",
			nicsEnvVal: "eth0,wlan0",
			expectedCfg: &NicConfig{
				nics:         map[string]struct{}{"eth0": {}, "wlan0": {}},
				isBlacklist:  false,
				hasWildcards: false,
			},
		},
		{
			name:       "Blacklist mode",
			nicsEnvVal: "-eth0,wlan0",
			expectedCfg: &NicConfig{
				nics:         map[string]struct{}{"eth0": {}, "wlan0": {}},
				isBlacklist:  true,
				hasWildcards: false,
			},
		},
		{
			name:       "With wildcards",
			nicsEnvVal: "eth*,wlan0",
			expectedCfg: &NicConfig{
				nics:         map[string]struct{}{"eth*": {}, "wlan0": {}},
				isBlacklist:  false,
				hasWildcards: true,
			},
		},
		{
			name:       "Blacklist with wildcards",
			nicsEnvVal: "-eth*,wlan0",
			expectedCfg: &NicConfig{
				nics:         map[string]struct{}{"eth*": {}, "wlan0": {}},
				isBlacklist:  true,
				hasWildcards: true,
			},
		},
		{
			name:       "With whitespace",
			nicsEnvVal: "eth0, wlan0 , eth1",
			expectedCfg: &NicConfig{
				nics:         map[string]struct{}{"eth0": {}, "wlan0": {}, "eth1": {}},
				isBlacklist:  false,
				hasWildcards: false,
			},
		},
		{
			name:       "Only wildcards",
			nicsEnvVal: "eth*,wlan*",
			expectedCfg: &NicConfig{
				nics:         map[string]struct{}{"eth*": {}, "wlan*": {}},
				isBlacklist:  false,
				hasWildcards: true,
			},
		},
		{
			name:       "Leading dash only",
			nicsEnvVal: "-",
			expectedCfg: &NicConfig{
				nics:         map[string]struct{}{},
				isBlacklist:  true,
				hasWildcards: false,
			},
		},
		{
			name:       "Mixed exact and wildcard",
			nicsEnvVal: "eth0,br-*",
			expectedCfg: &NicConfig{
				nics:         map[string]struct{}{"eth0": {}, "br-*": {}},
				isBlacklist:  false,
				hasWildcards: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newNicConfig(tt.nicsEnvVal)
			require.NotNil(t, cfg)
			assert.Equal(t, tt.expectedCfg.isBlacklist, cfg.isBlacklist)
			assert.Equal(t, tt.expectedCfg.hasWildcards, cfg.hasWildcards)
			assert.Equal(t, tt.expectedCfg.nics, cfg.nics)
		})
	}
}
func TestSkipNetworkInterface(t *testing.T) {
	tests := []struct {
		name       string
		nic        psutilNet.IOCountersStat
		nicCfg     *NicConfig
		expectSkip bool
	}{
		{"loopback lo", psutilNet.IOCountersStat{Name: "lo", BytesSent: 100, BytesRecv: 100}, nil, true},
		{"loopback lo0", psutilNet.IOCountersStat{Name: "lo0", BytesSent: 100, BytesRecv: 100}, nil, true},
		{"docker prefix", psutilNet.IOCountersStat{Name: "docker0", BytesSent: 100, BytesRecv: 100}, nil, true},
		{"br- prefix", psutilNet.IOCountersStat{Name: "br-lan", BytesSent: 100, BytesRecv: 100}, nil, true},
		{"veth prefix", psutilNet.IOCountersStat{Name: "veth0abc", BytesSent: 100, BytesRecv: 100}, nil, true},
		{"bond prefix", psutilNet.IOCountersStat{Name: "bond0", BytesSent: 100, BytesRecv: 100}, nil, true},
		{"cali prefix", psutilNet.IOCountersStat{Name: "cali1234", BytesSent: 100, BytesRecv: 100}, nil, true},
		{"zero BytesRecv", psutilNet.IOCountersStat{Name: "eth0", BytesSent: 100, BytesRecv: 0}, nil, false},
		{"zero BytesSent", psutilNet.IOCountersStat{Name: "eth0", BytesSent: 0, BytesRecv: 100}, nil, false},
		{"both zero", psutilNet.IOCountersStat{Name: "eth0", BytesSent: 0, BytesRecv: 0}, nil, false},
		{"normal eth0", psutilNet.IOCountersStat{Name: "eth0", BytesSent: 100, BytesRecv: 200}, nil, false},
		{"normal wlan0", psutilNet.IOCountersStat{Name: "wlan0", BytesSent: 1, BytesRecv: 1}, nil, false},
		{"whitelist overrides skip (docker)", psutilNet.IOCountersStat{Name: "docker0", BytesSent: 100, BytesRecv: 100}, newNicConfig("docker0"), false},
		{"whitelist overrides skip (lo)", psutilNet.IOCountersStat{Name: "lo", BytesSent: 100, BytesRecv: 100}, newNicConfig("lo"), false},
		{"whitelist exclusion", psutilNet.IOCountersStat{Name: "eth1", BytesSent: 100, BytesRecv: 100}, newNicConfig("eth0"), true},
		{"blacklist skip lo", psutilNet.IOCountersStat{Name: "lo", BytesSent: 100, BytesRecv: 100}, newNicConfig("-eth0"), true},
		{"blacklist explicit eth0", psutilNet.IOCountersStat{Name: "eth0", BytesSent: 100, BytesRecv: 100}, newNicConfig("-eth0"), true},
		{"blacklist allow eth1", psutilNet.IOCountersStat{Name: "eth1", BytesSent: 100, BytesRecv: 100}, newNicConfig("-eth0"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectSkip, skipNetworkInterface(tt.nic, tt.nicCfg))
		})
	}
}

func TestEnsureNetworkInterfacesMap(t *testing.T) {
	var m Manager
	var stats systemmodel.Stats

	// Initially nil
	assert.Nil(t, stats.NetworkInterfaces)
	// Ensure map is created
	m.ensureNetworkInterfacesMap(&stats)
	assert.NotNil(t, stats.NetworkInterfaces)
	// Idempotent
	m.ensureNetworkInterfacesMap(&stats)
	assert.NotNil(t, stats.NetworkInterfaces)
}

func TestLoadAndTickNetBaseline(t *testing.T) {
	m := &Manager{netIoStats: make(map[uint16]ioStats)}

	// First call initializes time and returns 0 elapsed
	ni, elapsed := m.loadAndTickNetBaseline(100)
	assert.Equal(t, uint64(0), elapsed)
	assert.False(t, ni.Time.IsZero())

	// Store back what loadAndTick returns to mimic updateNetworkStats behavior
	m.netIoStats[100] = ni

	time.Sleep(2 * time.Millisecond)

	// Next call should produce >= 0 elapsed and update time
	ni2, elapsed2 := m.loadAndTickNetBaseline(100)
	assert.Positive(t, elapsed2)
	assert.False(t, ni2.Time.IsZero())
}

func TestSumAndTrackPerNicDeltas(t *testing.T) {
	m := &Manager{
		netInterfaces:             map[string]struct{}{"eth0": {}, "wlan0": {}},
		netInterfaceDeltaTrackers: make(map[uint16]*deltatracker.DeltaTracker[string, uint64]),
	}

	// Two samples for same cache interval to verify delta behavior
	cache := uint16(42)
	net1 := []psutilNet.IOCountersStat{{Name: "eth0", BytesSent: 1000, BytesRecv: 2000}}
	stats1 := &systemmodel.Stats{}
	m.ensureNetworkInterfacesMap(stats1)
	tx1, rx1 := m.sumAndTrackPerNicDeltas(cache, 0, net1, stats1)
	assert.Zero(t, tx1)
	assert.Zero(t, rx1)

	// Second cycle with elapsed, larger counters -> deltas computed inside
	net2 := []psutilNet.IOCountersStat{{Name: "eth0", BytesSent: 4000, BytesRecv: 9000}}
	stats := &systemmodel.Stats{}
	m.ensureNetworkInterfacesMap(stats)
	tx2, rx2 := m.sumAndTrackPerNicDeltas(cache, 1000, net2, stats)
	assert.Equal(t, uint64(3000), tx2)
	assert.Equal(t, uint64(7000), rx2)
	// Up/Down deltas per second should be (4000-1000)/1s = 3000 and (9000-2000)/1s = 7000
	ni, ok := stats.NetworkInterfaces["eth0"]
	assert.True(t, ok)
	assert.Equal(t, uint64(3000), ni[0])
	assert.Equal(t, uint64(7000), ni[1])
}

func TestSumAndTrackPerNicDeltasToleratesVanishedInterface(t *testing.T) {
	m := &Manager{
		netInterfaces:             map[string]struct{}{"eth0": {}, "vnet0": {}},
		netInterfaceDeltaTrackers: make(map[uint16]*deltatracker.DeltaTracker[string, uint64]),
	}
	cache := uint16(5)

	first := []psutilNet.IOCountersStat{
		{Name: "eth0", BytesSent: 1_000, BytesRecv: 1_000},
		{Name: "vnet0", BytesSent: 9_000_000, BytesRecv: 9_000_000},
	}
	stats := &systemmodel.Stats{}
	m.ensureNetworkInterfacesMap(stats)
	_, _ = m.sumAndTrackPerNicDeltas(cache, 0, first, stats)

	// vnet0 disappears (VM stopped), so the summed counters drop.
	second := []psutilNet.IOCountersStat{{Name: "eth0", BytesSent: 3_000, BytesRecv: 5_000}}
	stats = &systemmodel.Stats{}
	m.ensureNetworkInterfacesMap(stats)
	up, down := m.sumAndTrackPerNicDeltas(cache, 1_000, second, stats)
	assert.Equal(t, uint64(2_000), up)
	assert.Equal(t, uint64(4_000), down)
}

func TestSumAndTrackPerNicDeltasHandlesCounterReset(t *testing.T) {
	m := &Manager{
		netInterfaces:             map[string]struct{}{"eth0": {}},
		netInterfaceDeltaTrackers: make(map[uint16]*deltatracker.DeltaTracker[string, uint64]),
	}

	cache := uint16(77)

	// First interval establishes baseline values
	initial := []psutilNet.IOCountersStat{{Name: "eth0", BytesSent: 4_000, BytesRecv: 6_000}}
	statsInitial := &systemmodel.Stats{}
	m.ensureNetworkInterfacesMap(statsInitial)
	_, _ = m.sumAndTrackPerNicDeltas(cache, 0, initial, statsInitial)

	// Second interval increments counters normally so previous snapshot gets populated
	increment := []psutilNet.IOCountersStat{{Name: "eth0", BytesSent: 9_000, BytesRecv: 11_000}}
	statsIncrement := &systemmodel.Stats{}
	m.ensureNetworkInterfacesMap(statsIncrement)
	_, _ = m.sumAndTrackPerNicDeltas(cache, 1_000, increment, statsIncrement)

	niIncrement, ok := statsIncrement.NetworkInterfaces["eth0"]
	require.True(t, ok)
	assert.Equal(t, uint64(5_000), niIncrement[0])
	assert.Equal(t, uint64(5_000), niIncrement[1])

	// Third interval simulates counter reset (values drop below previous totals)
	reset := []psutilNet.IOCountersStat{{Name: "eth0", BytesSent: 1_200, BytesRecv: 1_500}}
	statsReset := &systemmodel.Stats{}
	m.ensureNetworkInterfacesMap(statsReset)
	_, _ = m.sumAndTrackPerNicDeltas(cache, 1_000, reset, statsReset)

	niReset, ok := statsReset.NetworkInterfaces["eth0"]
	require.True(t, ok)
	assert.Equal(t, uint64(1_200), niReset[0], "upload delta should match new counter value after reset")
	assert.Equal(t, uint64(1_500), niReset[1], "download delta should match new counter value after reset")
}

func TestNetworkReseedsStaleLiveKey(t *testing.T) {
	m := NewManager()
	collectorAt := time.Now().Add(-2 * time.Second)
	m.netIoStats[collector.TickKey] = ioStats{Time: collectorAt}
	tracker := deltatracker.NewDeltaTracker[string, uint64]()
	tracker.Set("eth0up", 500)
	tracker.Cycle()
	m.netInterfaceDeltaTrackers[collector.TickKey] = tracker
	m.netIoStats[1010] = ioStats{Time: time.Now().Add(-time.Hour)}

	m.reseedFromCollector(1010)
	if !m.netIoStats[1010].Time.Equal(collectorAt) {
		t.Fatalf("baseline not reseeded: %+v", m.netIoStats[1010])
	}
	if prev, ok := m.netInterfaceDeltaTrackers[1010].Previous("eth0up"); !ok || prev != 500 {
		t.Fatalf("tracker not cloned: %d %v", prev, ok)
	}
	// The clone must be independent of the collector's tracker.
	m.netInterfaceDeltaTrackers[1010].Set("eth0up", 900)
	if _, ok := m.netInterfaceDeltaTrackers[collector.TickKey].Previous("eth0up"); !ok {
		t.Fatal("collector tracker lost its baseline")
	}
	assert.NotSame(t, tracker, m.netInterfaceDeltaTrackers[1010])
}

func TestNetworkReseedKeepsFreshLiveKeyAndCollectorKey(t *testing.T) {
	m := NewManager()
	collectorAt := time.Now().Add(-time.Minute)
	liveAt := time.Now()
	m.netIoStats[collector.TickKey] = ioStats{Time: collectorAt}
	m.netIoStats[1010] = ioStats{Time: liveAt}

	m.reseedFromCollector(1010)
	assert.Equal(t, liveAt, m.netIoStats[1010].Time, "fresh live baseline must be kept")

	m.reseedFromCollector(collector.TickKey)
	assert.Equal(t, collectorAt, m.netIoStats[collector.TickKey].Time)
	assert.Len(t, m.netIoStats, 2)
}

func TestNetworkReseedSkipsCollectorTickInsideMinWindow(t *testing.T) {
	m := NewManager()
	liveAt := time.Now().Add(-time.Hour)
	m.netIoStats[collector.TickKey] = ioStats{Time: time.Now().Add(-100 * time.Millisecond)}
	m.netIoStats[1010] = ioStats{Time: liveAt}

	m.reseedFromCollector(1010)
	assert.Equal(t, liveAt, m.netIoStats[1010].Time,
		"a collector tick under one second old would amplify the rate by 1000/delta")
}
