// Package network collects interface bandwidth, connection and IRQ statistics.
package network

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	psutilNet "github.com/shirou/gopsutil/v4/net"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/config"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/deltatracker"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

// ioStats is the sample-time baseline of one cache interval; the byte-counter
// baselines live in the per-NIC delta trackers.
type ioStats struct {
	Time time.Time
}

// Manager owns network interface tracking and per-cache-time NIC delta state.
type Manager struct {
	netInterfaces             map[string]struct{}                                   // Valid network interfaces
	netIoStats                map[uint16]ioStats                                    // Bandwidth usage per cache interval
	netInterfaceDeltaTrackers map[uint16]*deltatracker.DeltaTracker[string, uint64] // Per-cache-time NIC delta trackers
}

// NewManager constructs a Manager with empty tracking maps.
func NewManager() *Manager {
	return &Manager{
		netInterfaces:             make(map[string]struct{}),
		netIoStats:                make(map[uint16]ioStats),
		netInterfaceDeltaTrackers: make(map[uint16]*deltatracker.DeltaTracker[string, uint64]),
	}
}

// NicConfig controls inclusion/exclusion of network interfaces via the NICS env var
//
// Behavior mirrors SensorConfig's matching logic:
// - Leading '-' means blacklist mode; otherwise whitelist mode
// - Supports '*' wildcards using path.Match
// - In whitelist mode with an empty list, no NICs are selected
// - In blacklist mode with an empty list, all NICs are selected
type NicConfig struct {
	nics         map[string]struct{}
	isBlacklist  bool
	hasWildcards bool
}

func newNicConfig(nicsEnvVal string) *NicConfig {
	cfg := &NicConfig{
		nics: make(map[string]struct{}),
	}
	if strings.HasPrefix(nicsEnvVal, "-") {
		cfg.isBlacklist = true
		nicsEnvVal = nicsEnvVal[1:]
	}
	for nic := range strings.SplitSeq(nicsEnvVal, ",") {
		nic = strings.TrimSpace(nic)
		if nic != "" {
			cfg.nics[nic] = struct{}{}
			if strings.Contains(nic, "*") {
				cfg.hasWildcards = true
			}
		}
	}
	return cfg
}

// isValidNic determines if a NIC should be included based on NicConfig rules
func isValidNic(nicName string, cfg *NicConfig) bool {
	// Empty list behavior differs by mode: blacklist: allow all; whitelist: allow none
	if len(cfg.nics) == 0 {
		return cfg.isBlacklist
	}

	// Exact match: return true if whitelist, false if blacklist
	if _, exactMatch := cfg.nics[nicName]; exactMatch {
		return !cfg.isBlacklist
	}

	// If no wildcards, return true if blacklist, false if whitelist
	if !cfg.hasWildcards {
		return cfg.isBlacklist
	}

	// Check for wildcard patterns
	for pattern := range cfg.nics {
		if !strings.Contains(pattern, "*") {
			continue
		}
		if match, _ := path.Match(pattern, nicName); match {
			return !cfg.isBlacklist
		}
	}

	return cfg.isBlacklist
}

// UpdateStats updates bandwidth and per-interface rates in the stats.
func (m *Manager) UpdateStats(ctx context.Context, cacheTimeMs uint16, systemStats *systemmodel.Stats) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.ensureNetworkInterfacesMap(systemStats)

	if netIO, err := psutilNet.IOCountersWithContext(ctx, true); err == nil {
		m.refreshNetInterfaces(netIO)
		m.reseedFromCollector(cacheTimeMs)
		nis, msElapsed := m.loadAndTickNetBaseline(cacheTimeMs)
		systemStats.Bandwidth[0], systemStats.Bandwidth[1] = m.sumAndTrackPerNicDeltas(cacheTimeMs, msElapsed, netIO, systemStats)
		m.netIoStats[cacheTimeMs] = nis
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return nil
}

// reseedFromCollector replaces a live key's NIC baseline with the collector's
// when the live baseline predates the last collector tick and that tick is at
// least collector.ReseedMinWindow old, so a request after idle averages over at most one
// collector interval without collapsing onto a millisecond window.
func (m *Manager) reseedFromCollector(cacheTimeMs uint16) {
	if cacheTimeMs == collector.TickKey {
		return
	}
	base := m.netIoStats[collector.TickKey]
	if !collector.ShouldReseed(m.netIoStats[cacheTimeMs].Time, base.Time, time.Now()) {
		return
	}
	m.netIoStats[cacheTimeMs] = base
	if tracker := m.netInterfaceDeltaTrackers[collector.TickKey]; tracker != nil {
		m.netInterfaceDeltaTrackers[cacheTimeMs] = tracker.Clone()
	}
}

// Initialize records the starting counters and the valid interfaces.
func (m *Manager) Initialize(ctx context.Context) {
	// reset valid network interfaces
	m.netInterfaces = make(map[string]struct{}, 0)

	if netIO, err := psutilNet.IOCountersWithContext(ctx, true); err == nil {
		m.refreshNetInterfaces(netIO)
	}

	// Reset per-cache-time trackers and baselines so they will reinitialize on next use
	m.netInterfaceDeltaTrackers = make(map[uint16]*deltatracker.DeltaTracker[string, uint64])
	m.netIoStats = make(map[uint16]ioStats)
}

func (m *Manager) refreshNetInterfaces(netIO []psutilNet.IOCountersStat) {
	if m.netInterfaces == nil {
		m.netInterfaces = make(map[string]struct{})
	}

	nicsEnvVal, nicsEnvExists := config.LookupEnv(config.EnvNICs)
	var nicCfg *NicConfig
	if nicsEnvExists {
		nicCfg = newNicConfig(nicsEnvVal)
	}

	seen := make(map[string]struct{}, len(netIO))
	for _, v := range netIO {
		if skipNetworkInterface(v, nicCfg) {
			continue
		}
		seen[v.Name] = struct{}{}
		if _, exists := m.netInterfaces[v.Name]; !exists {
			slog.Debug("network interface detected", "name", v.Name, "sent", v.BytesSent, "recv", v.BytesRecv)
		}
		m.netInterfaces[v.Name] = struct{}{}
	}
	for name := range m.netInterfaces {
		if _, ok := seen[name]; !ok {
			delete(m.netInterfaces, name)
		}
	}
}

// ensureNetworkInterfacesMap ensures systemStats.NetworkInterfaces map exists
func (m *Manager) ensureNetworkInterfacesMap(systemStats *systemmodel.Stats) {
	if systemStats.NetworkInterfaces == nil {
		systemStats.NetworkInterfaces = make(map[string][4]uint64, 0)
	}
	if systemStats.NetworkInterfaceCounters == nil {
		systemStats.NetworkInterfaceCounters = make(map[string]systemmodel.NetworkInterfaceCounters)
	}
}

// loadAndTickNetBaseline returns the ioStats baseline and milliseconds elapsed, updating time
func (m *Manager) loadAndTickNetBaseline(cacheTimeMs uint16) (netIoStat ioStats, msElapsed uint64) {
	netIoStat = m.netIoStats[cacheTimeMs]
	if netIoStat.Time.IsZero() {
		netIoStat.Time = time.Now()
		msElapsed = 0
	} else {
		msElapsed = uint64(time.Since(netIoStat.Time).Milliseconds())
		netIoStat.Time = time.Now()
	}
	return netIoStat, msElapsed
}

// sumAndTrackPerNicDeltas records per-NIC up/down rates into systemStats and
// returns their sums, so an interface that disappears or resets its counters
// never drives the total negative.
//
//nolint:gocognit // Network delta aggregation handles multiple filtering and tracker edge cases.
func (m *Manager) sumAndTrackPerNicDeltas(cacheTimeMs uint16, msElapsed uint64, netIO []psutilNet.IOCountersStat, systemStats *systemmodel.Stats) (totalUp, totalDown uint64) {
	tracker := m.netInterfaceDeltaTrackers[cacheTimeMs]
	if tracker == nil {
		tracker = deltatracker.NewDeltaTracker[string, uint64]()
		m.netInterfaceDeltaTrackers[cacheTimeMs] = tracker
	}
	tracker.Cycle()

	for _, v := range netIO {
		if _, exists := m.netInterfaces[v.Name]; !exists {
			continue
		}
		var upDelta, downDelta uint64
		upKey, downKey := fmt.Sprintf("%sup", v.Name), fmt.Sprintf("%sdown", v.Name)
		tracker.Set(upKey, v.BytesSent)
		tracker.Set(downKey, v.BytesRecv)
		if msElapsed > 0 {
			if prevVal, ok := tracker.Previous(upKey); ok {
				var deltaBytes uint64
				if v.BytesSent >= prevVal {
					deltaBytes = v.BytesSent - prevVal
				} else {
					deltaBytes = v.BytesSent
				}
				upDelta = deltaBytes * 1000 / msElapsed
			}
			if prevVal, ok := tracker.Previous(downKey); ok {
				var deltaBytes uint64
				if v.BytesRecv >= prevVal {
					deltaBytes = v.BytesRecv - prevVal
				} else {
					deltaBytes = v.BytesRecv
				}
				downDelta = deltaBytes * 1000 / msElapsed
			}
		}
		totalUp += upDelta
		totalDown += downDelta
		systemStats.NetworkInterfaces[v.Name] = [4]uint64{upDelta, downDelta, v.BytesSent, v.BytesRecv}
		systemStats.NetworkInterfaceCounters[v.Name] = systemmodel.NetworkInterfaceCounters{
			RxDropped: v.Dropin, RxErrors: v.Errin, RxPackets: v.PacketsRecv,
			TxDropped: v.Dropout, TxErrors: v.Errout, TxPackets: v.PacketsSent,
		}
	}

	return totalUp, totalDown
}

// skipNetworkInterface returns true if the network interface should be ignored.
func skipNetworkInterface(v psutilNet.IOCountersStat, nicCfg *NicConfig) bool {
	if nicCfg != nil {
		if !isValidNic(v.Name, nicCfg) {
			return true
		}
		// In whitelist mode, we honor explicit inclusion without auto-filtering.
		if !nicCfg.isBlacklist {
			return false
		}
		// In blacklist mode, still apply the auto-filter below.
	}

	switch {
	case strings.HasPrefix(v.Name, "lo"),
		strings.HasPrefix(v.Name, "docker"),
		strings.HasPrefix(v.Name, "br-"),
		strings.HasPrefix(v.Name, "veth"),
		strings.HasPrefix(v.Name, "bond"),
		strings.HasPrefix(v.Name, "cali"):
		return true
	default:
		return false
	}
}
