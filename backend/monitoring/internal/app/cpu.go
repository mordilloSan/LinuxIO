package app

import (
	"context"
	"math"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

var lastCpuTimes = make(map[uint16]cpu.TimesStat)
var lastPerCoreCpuTimes = make(map[uint16][]cpu.TimesStat)
var lastCpuSampleAt = make(map[uint16]time.Time)
var cpuTimes = cpu.TimesWithContext

// initializeCpuMetrics stores initial CPU times for the default 60-second cache interval.
func initializeCpuMetrics(ctx context.Context) {
	if times, err := cpuTimes(ctx, false); err == nil && len(times) > 0 {
		lastCpuTimes[60000] = times[0]
		lastCpuSampleAt[60000] = time.Now()
	}
	if perCoreTimes, err := cpuTimes(ctx, true); err == nil && len(perCoreTimes) > 0 {
		lastPerCoreCpuTimes[60000] = perCoreTimes
		lastCpuSampleAt[60000] = time.Now()
	}
}

// reseedCPUFromCollector replaces a live key's baseline with the collector's
// when the live baseline predates the last collector tick and that tick is at
// least reseedMinWindow old, so a request after idle averages over at most one
// collector interval without collapsing onto a millisecond window.
func reseedCPUFromCollector(cacheTimeMs uint16) {
	if cacheTimeMs == collectorDataKeyMs {
		return
	}
	collectorAt := lastCpuSampleAt[collectorDataKeyMs]
	if !shouldReseedFromCollector(lastCpuSampleAt[cacheTimeMs], collectorAt, time.Now()) {
		return
	}
	if times, ok := lastCpuTimes[collectorDataKeyMs]; ok {
		lastCpuTimes[cacheTimeMs] = times
	}
	if perCore, ok := lastPerCoreCpuTimes[collectorDataKeyMs]; ok {
		lastPerCoreCpuTimes[cacheTimeMs] = append([]cpu.TimesStat(nil), perCore...)
	}
	lastCpuSampleAt[cacheTimeMs] = collectorAt
}

// CpuMetrics contains detailed CPU usage breakdown
type CpuMetrics struct {
	Total  float64
	User   float64
	System float64
	Iowait float64
	Steal  float64
	Idle   float64
}

// getCpuMetrics calculates detailed CPU usage metrics using cached previous measurements.
// It returns percentages for total, user, system, iowait, and steal time.
func getCpuMetrics(ctx context.Context, cacheTimeMs uint16) (CpuMetrics, error) {
	times, err := cpuTimes(ctx, false)
	if err != nil || len(times) == 0 {
		return CpuMetrics{}, err
	}
	reseedCPUFromCollector(cacheTimeMs)
	// if cacheTimeMs is not in lastCpuTimes, use 60000 as fallback lastCpuTime
	if _, ok := lastCpuTimes[cacheTimeMs]; !ok {
		if _, hasDefault := lastCpuTimes[60000]; !hasDefault {
			lastCpuTimes[60000] = times[0]
		}
		lastCpuTimes[cacheTimeMs] = lastCpuTimes[60000]
	}

	t1 := lastCpuTimes[cacheTimeMs]
	t2 := times[0]

	t1All, _ := getAllBusy(t1)
	t2All, _ := getAllBusy(t2)

	totalDelta := t2All - t1All
	if totalDelta <= 0 {
		return CpuMetrics{}, nil
	}

	metrics := CpuMetrics{
		Total:  calculateBusy(t1, t2),
		User:   clampPercent((t2.User - t1.User) / totalDelta * 100),
		System: clampPercent((t2.System - t1.System) / totalDelta * 100),
		Iowait: clampPercent((t2.Iowait - t1.Iowait) / totalDelta * 100),
		Steal:  clampPercent((t2.Steal - t1.Steal) / totalDelta * 100),
		Idle:   clampPercent((t2.Idle - t1.Idle) / totalDelta * 100),
	}

	lastCpuTimes[cacheTimeMs] = times[0]
	lastCpuSampleAt[cacheTimeMs] = time.Now()
	return metrics, nil
}

// clampPercent ensures the percentage is between 0 and 100
func clampPercent(value float64) float64 {
	return math.Min(100, math.Max(0, value))
}

// getPerCoreCpuUsage calculates per-core CPU busy usage as integer percentages (0-100).
// It uses cached previous measurements for the provided cache interval.
func getPerCoreCpuUsage(ctx context.Context, cacheTimeMs uint16) (system.Uint8Slice, error) {
	perCoreTimes, err := cpuTimes(ctx, true)
	if err != nil || len(perCoreTimes) == 0 {
		return nil, err
	}

	reseedCPUFromCollector(cacheTimeMs)
	// Initialize cache if needed
	if _, ok := lastPerCoreCpuTimes[cacheTimeMs]; !ok {
		if _, hasDefault := lastPerCoreCpuTimes[60000]; !hasDefault {
			lastPerCoreCpuTimes[60000] = perCoreTimes
		}
		lastPerCoreCpuTimes[cacheTimeMs] = lastPerCoreCpuTimes[60000]
	}

	lastTimes := lastPerCoreCpuTimes[cacheTimeMs]

	// Limit to the number of cores available in both samples
	length := min(len(lastTimes), len(perCoreTimes))

	usage := make([]uint8, length)
	for i := range length {
		t1 := lastTimes[i]
		t2 := perCoreTimes[i]
		usage[i] = uint8(math.Round(calculateBusy(t1, t2)))
	}

	lastPerCoreCpuTimes[cacheTimeMs] = perCoreTimes
	lastCpuSampleAt[cacheTimeMs] = time.Now()
	return usage, nil
}

// calculateBusy calculates the CPU busy percentage between two time points.
// It computes the ratio of busy time to total time elapsed between t1 and t2,
// returning a percentage clamped between 0 and 100.
func calculateBusy(t1, t2 cpu.TimesStat) float64 {
	t1All, t1Busy := getAllBusy(t1)
	t2All, t2Busy := getAllBusy(t2)

	if t2All <= t1All || t2Busy <= t1Busy {
		return 0
	}
	return clampPercent((t2Busy - t1Busy) / (t2All - t1All) * 100)
}

// getAllBusy calculates the total CPU time and busy CPU time from CPU times statistics.
// Excludes guest and guest_nice time from the total to match kernel behavior.
// Returns total CPU time and busy CPU time (total minus idle and I/O wait time).
func getAllBusy(t cpu.TimesStat) (float64, float64) {
	tot := t.User + t.System + t.Idle + t.Nice + t.Iowait + t.Irq + t.Softirq + t.Steal
	busy := tot - t.Idle - t.Iowait
	return tot, busy
}
