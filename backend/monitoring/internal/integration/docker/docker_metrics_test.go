package docker

import (
	"net/netip"
	"testing"
	"time"

	mobycontainer "github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/deltatracker"
)

func newMetricsManager() *Manager {
	dm := &Manager{}
	dm.initializeTracking()
	return dm
}

func TestMetricsCPUPercentHandlesInitialAndResetCounters(t *testing.T) {
	tests := []struct {
		name              string
		container         uint64
		system            uint64
		previousContainer uint64
		previousSystem    uint64
		want              float64
	}{
		{
			name:              "calculates percentage from deltas",
			container:         1500,
			system:            12000,
			previousContainer: 1000,
			previousSystem:    10000,
			want:              25,
		},
		{
			name:              "first sample establishes baseline",
			container:         1500,
			system:            12000,
			previousContainer: 0,
			previousSystem:    10000,
		},
		{
			name:              "zero system delta",
			container:         1500,
			system:            10000,
			previousContainer: 1000,
			previousSystem:    10000,
		},
		{
			name:              "container counter reset",
			container:         500,
			system:            12000,
			previousContainer: 1000,
			previousSystem:    10000,
		},
		{
			name:              "system counter reset",
			container:         1500,
			system:            9000,
			previousContainer: 1000,
			previousSystem:    10000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &mobycontainer.StatsResponse{CPUStats: mobycontainer.CPUStats{
				CPUUsage:    mobycontainer.CPUUsage{TotalUsage: tt.container},
				SystemUsage: tt.system,
			}}
			assert.InDelta(t, tt.want, calculateCPUPercentLinux(stats, tt.previousContainer, tt.previousSystem), 1e-9)
		})
	}
}

func TestMetricsMemoryUsageFallbackAndMaximum(t *testing.T) {
	tests := []struct {
		name      string
		usage     uint64
		inactive  uint64
		cache     uint64
		want      uint64
		expectErr bool
	}{
		{
			name:     "inactive file takes precedence",
			usage:    1 << 20,
			inactive: 1 << 18,
			cache:    1 << 19,
			want:     3 * (1 << 18),
		},
		{
			name:  "cache is used when inactive file is missing",
			usage: 1 << 20,
			cache: 1 << 18,
			want:  3 * (1 << 18),
		},
		{
			name:      "zero usage is invalid",
			expectErr: true,
		},
		{
			name:      "cache cannot exceed usage",
			usage:     10,
			inactive:  11,
			expectErr: true,
		},
		{
			name:      "maximum usage guard",
			usage:     maxMemoryUsage + 1,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &mobycontainer.StatsResponse{MemoryStats: mobycontainer.MemoryStats{
				Usage: tt.usage,
				Stats: map[string]uint64{
					"inactive_file": tt.inactive,
					"cache":         tt.cache,
				},
			}}
			got, err := calculateMemoryUsage(stats)
			if tt.expectErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMetricsNetworkRatesResetAndContainerIsolation(t *testing.T) {
	dm := newMetricsManager()
	const cacheTimeMs = uint16(30000)

	baseline := map[string]mobycontainer.NetworkStats{
		"eth0": {TxBytes: 1_000_000, RxBytes: 500_000},
		"eth1": {TxBytes: 250_000, RxBytes: 125_000},
	}
	assert.Equal(t, [2]uint64{0, 0}, func() [2]uint64 {
		sent, recv := dm.calculateNetworkStats("one", baseline, "one", cacheTimeMs)
		return [2]uint64{sent, recv}
	}())

	dm.lastNetworkReadTime[cacheTimeMs] = map[string]time.Time{
		"one": time.Now().Add(-time.Second),
		"two": time.Now().Add(-time.Second),
	}
	dm.calculateNetworkStats("two", baseline, "two", cacheTimeMs)
	dm.cycleNetworkDeltasForCacheTime(cacheTimeMs)

	next := map[string]mobycontainer.NetworkStats{
		"eth0": {TxBytes: 3_000_000, RxBytes: 1_500_000},
		"eth1": {TxBytes: 500_000, RxBytes: 250_000},
	}
	sent, recv := dm.calculateNetworkStats("one", next, "one", cacheTimeMs)
	assert.Greater(t, sent, uint64(2_100_000))
	assert.LessOrEqual(t, sent, uint64(2_250_000))
	assert.Greater(t, recv, uint64(1_000_000))
	assert.LessOrEqual(t, recv, uint64(1_125_000))
	unchangedSent, unchangedRecv := dm.calculateNetworkStats("two", baseline, "two", cacheTimeMs)
	assert.Equal(t, uint64(0), unchangedSent)
	assert.Equal(t, uint64(0), unchangedRecv)

	dm.cycleNetworkDeltasForCacheTime(cacheTimeMs)
	dm.lastNetworkReadTime[cacheTimeMs]["one"] = time.Now().Add(-time.Second)
	txOnly := map[string]mobycontainer.NetworkStats{
		"eth0": {TxBytes: 3_500_000, RxBytes: 1_500_000},
		"eth1": {TxBytes: 500_000, RxBytes: 250_000},
	}
	sent, recv = dm.calculateNetworkStats("one", txOnly, "one", cacheTimeMs)
	assert.Greater(t, sent, uint64(450_000))
	assert.LessOrEqual(t, sent, uint64(500_000))
	assert.Equal(t, uint64(0), recv)

	// A counter reset produces a wrapped delta; the speed guard rejects it.
	dm.cycleNetworkDeltasForCacheTime(cacheTimeMs)
	dm.lastNetworkReadTime[cacheTimeMs]["one"] = time.Now().Add(-time.Second)
	reset := map[string]mobycontainer.NetworkStats{"eth0": {TxBytes: 10, RxBytes: 10}}
	sent, recv = dm.calculateNetworkStats("one", reset, "one", cacheTimeMs)
	assert.Equal(t, uint64(0), sent)
	assert.Equal(t, uint64(0), recv)
}

func TestMetricsNetworkCacheTimeIsolation(t *testing.T) {
	dm := newMetricsManager()
	const fastCache, slowCache = uint16(1000), uint16(60000)
	baseline := map[string]mobycontainer.NetworkStats{"eth0": {TxBytes: 100, RxBytes: 100}}

	dm.calculateNetworkStats("one", baseline, "one", fastCache)
	dm.calculateNetworkStats("one", baseline, "one", slowCache)
	now := time.Now()
	dm.lastNetworkReadTime[fastCache] = map[string]time.Time{"one": now}
	dm.lastNetworkReadTime[slowCache] = map[string]time.Time{"one": now}
	dm.cycleNetworkDeltasForCacheTime(fastCache)
	dm.cycleNetworkDeltasForCacheTime(slowCache)

	for i := uint64(1); i <= 5; i++ {
		dm.lastNetworkReadTime[fastCache]["one"] = time.Now().Add(-time.Second)
		sent, _ := dm.calculateNetworkStats("one", map[string]mobycontainer.NetworkStats{
			"eth0": {TxBytes: 100 + i*10, RxBytes: 100 + i*10},
		}, "one", fastCache)
		assert.InDelta(t, 10, sent, 1)
		dm.cycleNetworkDeltasForCacheTime(fastCache)
	}

	dm.lastNetworkReadTime[slowCache]["one"] = time.Now().Add(-5 * time.Second)
	sent, _ := dm.calculateNetworkStats("one", map[string]mobycontainer.NetworkStats{
		"eth0": {TxBytes: 150, RxBytes: 150},
	}, "one", slowCache)
	assert.InDelta(t, 10, sent, 1)
}

func TestMetricsReseedCopiesAndClonesCollectorBaselines(t *testing.T) {
	dm := newMetricsManager()
	const collectorKey, liveKey = uint16(60000), uint16(1010)
	dm.initializeCpuTracking(collectorKey)
	dm.initializeCpuTracking(liveKey)
	dm.lastCpuContainer[collectorKey]["abc"] = 900
	dm.lastCpuSystem[collectorKey]["abc"] = 950
	collectorAt := time.Now().Add(-2 * time.Second)
	dm.lastNetworkReadTime[collectorKey] = map[string]time.Time{"abc": collectorAt}
	dm.lastCpuContainer[liveKey]["abc"] = 1
	dm.lastNetworkReadTime[liveKey] = map[string]time.Time{"abc": time.Now().Add(-time.Hour)}

	sent := deltatracker.NewDeltaTracker[string, uint64]()
	sent.Set("abc", 700)
	sent.Cycle()
	dm.networkSentTrackers[collectorKey] = sent
	recv := deltatracker.NewDeltaTracker[string, uint64]()
	recv.Set("abc", 800)
	recv.Cycle()
	dm.networkRecvTrackers[collectorKey] = recv

	dm.ReseedFromCollector(liveKey, collectorKey)
	assert.Equal(t, uint64(900), dm.lastCpuContainer[liveKey]["abc"])
	assert.Equal(t, uint64(950), dm.lastCpuSystem[liveKey]["abc"])
	assert.Equal(t, dm.lastNetworkReadTime[collectorKey]["abc"], dm.lastNetworkReadTime[liveKey]["abc"])

	sentPrevious, ok := dm.networkSentTrackers[liveKey].Previous("abc")
	require.True(t, ok)
	assert.Equal(t, uint64(700), sentPrevious)
	recvPrevious, ok := dm.networkRecvTrackers[liveKey].Previous("abc")
	require.True(t, ok)
	assert.Equal(t, uint64(800), recvPrevious)
	assert.NotSame(t, sent, dm.networkSentTrackers[liveKey])

	dm.lastCpuContainer[liveKey]["abc"] = 5
	liveAt := time.Now()
	dm.lastNetworkReadTime[liveKey]["abc"] = liveAt
	dm.networkSentTrackers[liveKey].Set("abc", 1000)
	assert.Equal(t, uint64(900), dm.lastCpuContainer[collectorKey]["abc"])
	assert.Equal(t, collectorAt, dm.lastNetworkReadTime[collectorKey]["abc"])
	assert.NotEqual(t, liveAt, dm.lastNetworkReadTime[collectorKey]["abc"])
	collectorPrevious, ok := dm.networkSentTrackers[collectorKey].Previous("abc")
	require.True(t, ok)
	assert.Equal(t, uint64(700), collectorPrevious)
}

func TestMetricsReseedKeepsFreshAndRecentCollectorWindows(t *testing.T) {
	t.Run("fresh live baseline", func(t *testing.T) {
		dm := newMetricsManager()
		const collectorKey, liveKey = uint16(60000), uint16(1010)
		dm.initializeCpuTracking(collectorKey)
		dm.initializeCpuTracking(liveKey)
		dm.lastCpuContainer[collectorKey]["abc"] = 900
		dm.lastCpuContainer[liveKey]["abc"] = 1
		dm.lastNetworkReadTime[collectorKey] = map[string]time.Time{"abc": time.Now().Add(-time.Minute)}
		dm.lastNetworkReadTime[liveKey] = map[string]time.Time{"abc": time.Now()}

		dm.ReseedFromCollector(liveKey, collectorKey)
		assert.Equal(t, uint64(1), dm.lastCpuContainer[liveKey]["abc"])
	})

	t.Run("collector tick inside minimum window", func(t *testing.T) {
		dm := newMetricsManager()
		const collectorKey, liveKey = uint16(60000), uint16(1010)
		dm.initializeCpuTracking(collectorKey)
		dm.initializeCpuTracking(liveKey)
		dm.lastCpuContainer[collectorKey]["abc"] = 900
		dm.lastCpuContainer[liveKey]["abc"] = 1
		dm.lastNetworkReadTime[collectorKey] = map[string]time.Time{"abc": time.Now().Add(-100 * time.Millisecond)}
		dm.lastNetworkReadTime[liveKey] = map[string]time.Time{"abc": time.Now().Add(-time.Hour)}

		dm.ReseedFromCollector(liveKey, collectorKey)
		assert.Equal(t, uint64(1), dm.lastCpuContainer[liveKey]["abc"])
	})
}

func TestMetricsContainerExclusionPatterns(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		value    string
		want     bool
	}{
		{name: "empty", value: "any-container"},
		{name: "exact", patterns: []string{"test-web"}, value: "test-web", want: true},
		{name: "exact mismatch", patterns: []string{"test-web"}, value: "prod-web"},
		{name: "prefix wildcard", patterns: []string{"test-*"}, value: "test-web", want: true},
		{name: "suffix wildcard", patterns: []string{"*-staging"}, value: "myapp-staging", want: true},
		{name: "both wildcards", patterns: []string{"*-myapp-*"}, value: "test-myapp-staging", want: true},
		{name: "second pattern", patterns: []string{"test-*", "*-staging"}, value: "myapp-staging", want: true},
		{name: "no match", patterns: []string{"test-*", "*-staging"}, value: "prod-web"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dm := &Manager{excludeContainers: tt.patterns}
			assert.Equal(t, tt.want, dm.shouldExcludeContainer(tt.value))
		})
	}
}

func TestMetricsConvertContainerPortsToString(t *testing.T) {
	zero := netip.MustParseAddr("0.0.0.0")
	public := netip.MustParseAddr("1.2.3.4")
	tests := []struct {
		name  string
		ports []mobycontainer.PortSummary
		want  string
	}{
		{name: "empty"},
		{name: "default host address", ports: []mobycontainer.PortSummary{{IP: zero, PublicPort: 80}}, want: "80"},
		{name: "public host address", ports: []mobycontainer.PortSummary{{IP: public, PublicPort: 443}}, want: "1.2.3.4:443"},
		{name: "sorts ascending", ports: []mobycontainer.PortSummary{{IP: zero, PublicPort: 443}, {IP: zero, PublicPort: 80}, {IP: zero, PublicPort: 8080}}, want: "80, 443, 8080"},
		{name: "deduplicates public ports", ports: []mobycontainer.PortSummary{{IP: zero, PublicPort: 80}, {IP: zero, PublicPort: 80}, {IP: zero, PublicPort: 443}}, want: "80, 443"},
		{name: "skips zero port", ports: []mobycontainer.PortSummary{{IP: zero}, {IP: zero, PublicPort: 80}}, want: "80"},
		{name: "formats mixed addresses", ports: []mobycontainer.PortSummary{{IP: zero, PublicPort: 80}, {IP: public, PublicPort: 443}}, want: "80, 1.2.3.4:443"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, convertContainerPortsToString(tt.ports))
		})
	}
}
