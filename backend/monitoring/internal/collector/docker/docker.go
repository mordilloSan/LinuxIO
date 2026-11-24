// Package docker integrates with Docker and Podman Engine APIs.
package docker

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mobycontainer "github.com/moby/moby/api/types/container"
	mobysystem "github.com/moby/moby/api/types/system"
	mobyclient "github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/config"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/deltatracker"
	containermodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/units"
)

const (
	dockerTimeoutMs            = 2100
	minStatsPassTimeout        = 5 * time.Second
	maxStatsAttempts           = 5
	maxNetworkSpeedBps  uint64 = 5e9
	maxMemoryUsage      uint64 = 100 * 1024 * 1024 * 1024 * 1024
)

var errDockerClientUnavailable = errors.New("docker client unavailable")

type Manager struct {
	onPodmanDetected     func()
	client               *mobyclient.Client
	wg                   sync.WaitGroup
	sem                  chan struct{}
	containerStatsMutex  sync.RWMutex
	containerStatsMap    map[string]*containermodel.Stats
	validIds             map[string]struct{}
	goodDockerVersion    bool
	dockerVersionChecked bool
	excludeContainers    []string
	usingPodman          atomic.Bool
	libpodDetected       atomic.Bool

	lastCpuContainer map[uint16]map[string]uint64
	lastCpuSystem    map[uint16]map[string]uint64

	networkSentTrackers map[uint16]*deltatracker.DeltaTracker[string, uint64]
	networkRecvTrackers map[uint16]*deltatracker.DeltaTracker[string, uint64]
	lastNetworkReadTime map[uint16]map[string]time.Time

	// startReset holds the containers whose baselines were dropped in their
	// current first minute; see resetBaselinesOnStart.
	startReset map[string]struct{}

	// passTimeout bounds one stats pass; zero means minStatsPassTimeout.
	passTimeout time.Duration

	// collectorKey names the cache time the collector owns.
	collectorKey uint16
}

func (dm *Manager) queue(ctx context.Context) bool {
	dm.wg.Add(1)
	if dm.goodDockerVersion {
		select {
		case dm.sem <- struct{}{}:
		case <-ctx.Done():
			dm.wg.Done()
			return false
		}
	}
	return true
}

func (dm *Manager) dequeue() {
	dm.wg.Done()
	if dm.goodDockerVersion {
		<-dm.sem
	}
}

func (dm *Manager) shouldExcludeContainer(name string) bool {
	for _, pattern := range dm.excludeContainers {
		if match, _ := path.Match(pattern, name); match {
			return true
		}
	}
	return false
}

// GetStats returns stats for all running containers with cache-time-aware
// delta tracking. The pass, the list included, ends at its deadline
// (statsPassTimeout): a container whose stats fail is asked again, up to
// maxStatsAttempts times in all, and one that has not answered by then is left
// out of the sample rather than drawn as zeros.
func (dm *Manager) GetStats(ctx context.Context, cacheTimeMs uint16) ([]*containermodel.Stats, error) {
	if dm == nil || dm.client == nil {
		return nil, errDockerClientUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dm.initializeTracking()

	passTimeout := dm.passTimeout
	if passTimeout <= 0 {
		passTimeout = minStatsPassTimeout
	}
	passCtx, cancel := context.WithTimeout(ctx, passTimeout)
	defer cancel()

	list, err := dm.client.ContainerList(passCtx, mobyclient.ContainerListOptions{})
	if err != nil {
		return nil, err
	}
	dm.syncPodmanDetection()
	dm.ensureDockerVersionChecked(passCtx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	dm.ReseedFromCollector(cacheTimeMs, dm.collectorKey)
	containersLength := len(list.Items)
	pending := dm.sampledContainers(list.Items)

	var lastErr error
	for attempt := 1; attempt <= maxStatsAttempts && len(pending) > 0 && passCtx.Err() == nil; attempt++ {
		if attempt > 1 {
			slog.Debug("retrying failed containers", "count", len(pending), "attempt", attempt)
		}
		pending, lastErr = dm.updateContainers(passCtx, pending, cacheTimeMs, attempt)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(pending) > 0 {
		if lastErr == nil {
			lastErr = passCtx.Err()
		}
		names := make([]string, 0, len(pending))
		for _, ctr := range pending {
			names = append(names, containerName(ctr))
		}
		slog.Warn("container stats left out of the sample", "containers", names, "error", lastErr)
	}

	stats := make([]*containermodel.Stats, 0, containersLength)
	dm.containerStatsMutex.Lock()
	for id, value := range dm.containerStatsMap {
		if _, exists := dm.validIds[id]; !exists {
			delete(dm.containerStatsMap, id)
			continue
		}
		stats = append(stats, value)
	}
	dm.pruneBaselinesLocked()
	dm.containerStatsMutex.Unlock()

	dm.cycleNetworkDeltasForCacheTime(cacheTimeMs)
	return stats, nil
}

// sampledContainers records the containers this pass samples in validIds and
// returns them: the listed ones that are running or paused and not excluded.
func (dm *Manager) sampledContainers(items []mobycontainer.Summary) []*mobycontainer.Summary {
	dm.validIds = make(map[string]struct{}, len(items))
	sampled := make([]*mobycontainer.Summary, 0, len(items))
	for i := range items {
		ctr := &items[i]
		id := shortContainerID(ctr.ID)
		name := containerName(ctr)
		if dm.shouldExcludeContainer(name) {
			slog.Debug("excluding container", "name", name)
			continue
		}
		if !containerHasStats(ctr) {
			slog.Debug("skipping container that is not running", "name", name, "state", string(ctr.State))
			continue
		}
		dm.validIds[id] = struct{}{}
		dm.resetBaselinesOnStart(id, ctr.Status)
		sampled = append(sampled, ctr)
	}
	return sampled
}

// statsPassTimeout is how long one stats pass may take, the list included:
// five seconds, or two request timeouts when DOCKER_TIMEOUT sets a longer one.
// It bounds how long a daemon that stops answering holds the collection lock.
func statsPassTimeout(requestTimeout time.Duration) time.Duration {
	return max(minStatsPassTimeout, 2*requestTimeout)
}

// updateContainers reads the stats of ctrs, five at a time on Docker 25 and
// newer and Podman and all at once on older Docker, and returns those whose
// read failed or never started before the deadline, with the last of their
// errors. Their records are dropped, so the sample carries neither the zeros
// updateContainerStats wrote before it failed nor the previous pass's figures.
func (dm *Manager) updateContainers(ctx context.Context, ctrs []*mobycontainer.Summary, cacheTimeMs uint16, attempt int) ([]*mobycontainer.Summary, error) {
	var (
		failedMu sync.Mutex
		failed   []*mobycontainer.Summary
		lastErr  error
	)
	fail := func(ctr *mobycontainer.Summary, err error) {
		dm.containerStatsMutex.Lock()
		delete(dm.containerStatsMap, shortContainerID(ctr.ID))
		dm.containerStatsMutex.Unlock()
		failedMu.Lock()
		defer failedMu.Unlock()
		failed = append(failed, ctr)
		lastErr = err
	}
	for i, ctr := range ctrs {
		if !dm.queue(ctx) {
			for _, left := range ctrs[i:] {
				fail(left, ctx.Err())
			}
			break
		}
		go func() {
			defer dm.dequeue()
			labels := pprof.Labels("component", "docker", "container", shortContainerID(ctr.ID), "attempt", strconv.Itoa(attempt))
			pprof.Do(ctx, labels, func(ctx context.Context) {
				if err := dm.updateContainerStats(ctx, ctr, cacheTimeMs); err != nil {
					fail(ctr, err)
				}
			})
		}()
	}
	dm.wg.Wait()
	return failed, lastErr
}

// GetContainerIdentities returns the running containers known to the runtime.
func (dm *Manager) GetContainerIdentities(ctx context.Context) ([]containermodel.Identity, error) {
	if dm == nil {
		return nil, nil
	}
	if dm.client == nil {
		return nil, errDockerClientUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result, err := dm.client.ContainerList(ctx, mobyclient.ContainerListOptions{})
	if err != nil {
		return nil, err
	}
	dm.syncPodmanDetection()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	identities := make([]containermodel.Identity, 0, len(result.Items))
	for i := range result.Items {
		entry := &result.Items[i]
		if state := strings.TrimSpace(string(entry.State)); state != "" && !strings.EqualFold(state, "running") {
			continue
		}
		name := containerName(entry)
		if dm.shouldExcludeContainer(name) || strings.TrimSpace(entry.ID) == "" {
			continue
		}
		fullID := strings.TrimSpace(entry.ID)
		identities = append(identities, containermodel.Identity{
			ID:     shortContainerID(fullID),
			FullID: fullID,
			Name:   name,
		})
	}
	return identities, nil
}

// containerHasStats reports whether a listed container can answer a stats
// request. The list also returns a container Docker is restarting, which has
// no process between two runs: its request fails, is retried and fails again
// on every cycle. A paused container keeps its cgroup and is sampled, and so
// is one whose state the runtime does not report.
func containerHasStats(ctr *mobycontainer.Summary) bool {
	state := strings.TrimSpace(string(ctr.State))
	return state == "" || strings.EqualFold(state, "running") || strings.EqualFold(state, "paused")
}

func containerName(ctr *mobycontainer.Summary) string {
	if ctr == nil {
		return ""
	}
	for _, rawName := range ctr.Names {
		name := strings.TrimPrefix(strings.TrimSpace(rawName), "/")
		if name != "" {
			return name
		}
	}
	return ""
}

func shortContainerID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func (dm *Manager) initializeTracking() {
	dm.containerStatsMutex.Lock()
	defer dm.containerStatsMutex.Unlock()
	dm.initializeTrackingLocked()
	if dm.sem == nil {
		dm.sem = make(chan struct{}, 5)
	}
}

func (dm *Manager) initializeTrackingLocked() {
	if dm.containerStatsMap == nil {
		dm.containerStatsMap = make(map[string]*containermodel.Stats)
	}
	if dm.lastCpuContainer == nil {
		dm.lastCpuContainer = make(map[uint16]map[string]uint64)
	}
	if dm.lastCpuSystem == nil {
		dm.lastCpuSystem = make(map[uint16]map[string]uint64)
	}
	if dm.networkSentTrackers == nil {
		dm.networkSentTrackers = make(map[uint16]*deltatracker.DeltaTracker[string, uint64])
	}
	if dm.networkRecvTrackers == nil {
		dm.networkRecvTrackers = make(map[uint16]*deltatracker.DeltaTracker[string, uint64])
	}
	if dm.lastNetworkReadTime == nil {
		dm.lastNetworkReadTime = make(map[uint16]map[string]time.Time)
	}
}

func (dm *Manager) initializeCpuTracking(cacheTimeMs uint16) {
	if dm.lastCpuContainer == nil {
		dm.lastCpuContainer = make(map[uint16]map[string]uint64)
	}
	if dm.lastCpuSystem == nil {
		dm.lastCpuSystem = make(map[uint16]map[string]uint64)
	}
	if dm.lastCpuContainer[cacheTimeMs] == nil {
		dm.lastCpuContainer[cacheTimeMs] = make(map[string]uint64)
	}
	if dm.lastCpuSystem[cacheTimeMs] == nil {
		dm.lastCpuSystem[cacheTimeMs] = make(map[string]uint64)
	}
}

func (dm *Manager) getCpuPreviousValues(cacheTimeMs uint16, containerID string) (uint64, uint64) {
	return dm.lastCpuContainer[cacheTimeMs][containerID], dm.lastCpuSystem[cacheTimeMs][containerID]
}

func (dm *Manager) setCpuCurrentValues(cacheTimeMs uint16, containerID string, cpuContainer, cpuSystem uint64) {
	dm.lastCpuContainer[cacheTimeMs][containerID] = cpuContainer
	dm.lastCpuSystem[cacheTimeMs][containerID] = cpuSystem
}

func calculateCPUPercentLinux(stats *mobycontainer.StatsResponse, previousContainer, previousSystem uint64) float64 {
	if stats == nil || stats.CPUStats.CPUUsage.TotalUsage < previousContainer || stats.CPUStats.SystemUsage < previousSystem {
		return 0
	}
	cpuDelta := stats.CPUStats.CPUUsage.TotalUsage - previousContainer
	systemDelta := stats.CPUStats.SystemUsage - previousSystem
	if systemDelta == 0 || previousContainer == 0 {
		return 0
	}
	return float64(cpuDelta) / float64(systemDelta) * 100
}

func calculateMemoryUsage(stats *mobycontainer.StatsResponse) (uint64, error) {
	if stats == nil {
		return 0, errors.New("bad memory stats")
	}
	memCache := stats.MemoryStats.Stats["inactive_file"]
	if memCache == 0 {
		memCache = stats.MemoryStats.Stats["cache"]
	}
	if stats.MemoryStats.Usage <= memCache {
		return 0, errors.New("bad memory stats")
	}
	usedMemory := stats.MemoryStats.Usage - memCache
	if usedMemory > maxMemoryUsage {
		return 0, errors.New("bad memory stats")
	}
	return usedMemory, nil
}

func (dm *Manager) getNetworkTracker(cacheTimeMs uint16, sent bool) *deltatracker.DeltaTracker[string, uint64] {
	if sent {
		if dm.networkSentTrackers == nil {
			dm.networkSentTrackers = make(map[uint16]*deltatracker.DeltaTracker[string, uint64])
		}
		if dm.networkSentTrackers[cacheTimeMs] == nil {
			dm.networkSentTrackers[cacheTimeMs] = deltatracker.NewDeltaTracker[string, uint64]()
		}
		return dm.networkSentTrackers[cacheTimeMs]
	}
	if dm.networkRecvTrackers == nil {
		dm.networkRecvTrackers = make(map[uint16]*deltatracker.DeltaTracker[string, uint64])
	}
	if dm.networkRecvTrackers[cacheTimeMs] == nil {
		dm.networkRecvTrackers[cacheTimeMs] = deltatracker.NewDeltaTracker[string, uint64]()
	}
	return dm.networkRecvTrackers[cacheTimeMs]
}

func (dm *Manager) cycleNetworkDeltasForCacheTime(cacheTimeMs uint16) {
	if dm.networkSentTrackers != nil && dm.networkSentTrackers[cacheTimeMs] != nil {
		dm.networkSentTrackers[cacheTimeMs].Cycle()
	}
	if dm.networkRecvTrackers != nil && dm.networkRecvTrackers[cacheTimeMs] != nil {
		dm.networkRecvTrackers[cacheTimeMs].Cycle()
	}
}

func (dm *Manager) calculateNetworkStats(containerID string, networks map[string]mobycontainer.NetworkStats, name string, cacheTimeMs uint16) (uint64, uint64) {
	var totalSent, totalRecv uint64
	for _, network := range networks {
		totalSent += network.TxBytes
		totalRecv += network.RxBytes
	}

	sentTracker := dm.getNetworkTracker(cacheTimeMs, true)
	recvTracker := dm.getNetworkTracker(cacheTimeMs, false)
	sentTracker.Set(containerID, totalSent)
	recvTracker.Set(containerID, totalRecv)
	sentDeltaRaw := sentTracker.Delta(containerID)
	recvDeltaRaw := recvTracker.Delta(containerID)

	previousRead, ok := dm.lastNetworkReadTime[cacheTimeMs][containerID]
	if !ok {
		return 0, 0
	}
	elapsedMs := time.Since(previousRead).Milliseconds()
	if elapsedMs <= 0 {
		return 0, 0
	}
	sentDelta := sentDeltaRaw * 1000 / uint64(elapsedMs)
	recvDelta := recvDeltaRaw * 1000 / uint64(elapsedMs)
	if sentDelta > maxNetworkSpeedBps {
		slog.Warn("bad network delta", "container", name)
		sentDelta = 0
	}
	if recvDelta > maxNetworkSpeedBps {
		slog.Warn("bad network delta", "container", name)
		recvDelta = 0
	}
	return sentDelta, recvDelta
}

func validateCpuPercentage(cpuPct float64, containerName string) error {
	if cpuPct > 100 {
		return fmt.Errorf("%s cpu pct greater than 100: %+v", containerName, cpuPct)
	}
	return nil
}

func updateContainerStatsValues(stats *containermodel.Stats, cpuPct float64, usedMemory uint64, sentDelta, recvDelta uint64, readTime time.Time) {
	stats.Cpu = units.TwoDecimals(cpuPct)
	stats.Mem = units.BytesToMegabytes(float64(usedMemory))
	stats.Bandwidth = [2]uint64{sentDelta, recvDelta}
	stats.PrevReadTime = readTime
}

func convertContainerPortsToString(ports []mobycontainer.PortSummary) string {
	if len(ports) == 0 {
		return ""
	}
	sort.Slice(ports, func(i, j int) bool {
		return ports[i].PublicPort < ports[j].PublicPort
	})
	var builder strings.Builder
	seenPorts := make(map[uint16]struct{}, len(ports))
	for _, port := range ports {
		if port.PublicPort == 0 {
			continue
		}
		if _, exists := seenPorts[port.PublicPort]; exists {
			continue
		}
		seenPorts[port.PublicPort] = struct{}{}
		if builder.Len() > 0 {
			builder.WriteString(", ")
		}
		ip := port.IP.String()
		if port.IP.IsValid() && ip != "0.0.0.0" && ip != "::" {
			builder.WriteString(ip)
			builder.WriteByte(':')
		}
		builder.WriteString(strconv.Itoa(int(port.PublicPort)))
	}
	return builder.String()
}

func parseDockerStatus(status string) (string, containermodel.DockerHealth) {
	trimmed := strings.TrimSpace(status)
	if trimmed == "" {
		return "", containermodel.DockerHealthNone
	}
	trimmed = strings.Replace(trimmed, "About ", "", 1)

	statusText, healthText, found := strings.CutLast(trimmed, "(")
	if !found || !strings.HasSuffix(trimmed, ")") {
		return trimmed, containermodel.DockerHealthNone
	}
	statusText = strings.TrimSpace(statusText)
	if statusText == "" {
		statusText = trimmed
	}
	healthText = strings.TrimSpace(strings.TrimSuffix(healthText, ")"))
	if colonIndex := strings.IndexRune(healthText, ':'); colonIndex != -1 {
		prefix := strings.ToLower(strings.TrimSpace(healthText[:colonIndex]))
		if prefix == "health" || prefix == "health status" {
			healthText = strings.TrimSpace(healthText[colonIndex+1:])
		}
	}
	if health, ok := parseDockerHealthStatus(healthText); ok {
		return statusText, health
	}
	return trimmed, containermodel.DockerHealthNone
}

func parseDockerHealthStatus(status string) (containermodel.DockerHealth, bool) {
	health, ok := containermodel.DockerHealthStrings[strings.ToLower(strings.TrimSpace(status))]
	return health, ok
}

func (dm *Manager) getPodmanContainerHealth(ctx context.Context, containerID string) (containermodel.DockerHealth, error) {
	if dm == nil || dm.client == nil {
		return containermodel.DockerHealthNone, errDockerClientUnavailable
	}
	result, err := dm.client.ContainerInspect(ctx, containerID, mobyclient.ContainerInspectOptions{})
	if err != nil {
		return containermodel.DockerHealthNone, err
	}
	if result.Container.State == nil || result.Container.State.Health == nil {
		return containermodel.DockerHealthNone, nil
	}
	health, ok := parseDockerHealthStatus(string(result.Container.State.Health.Status))
	if !ok {
		return containermodel.DockerHealthNone, nil
	}
	return health, nil
}

func (dm *Manager) updateContainerStats(ctx context.Context, ctr *mobycontainer.Summary, cacheTimeMs uint16) error {
	if ctr == nil || dm == nil || dm.client == nil {
		return errDockerClientUnavailable
	}
	name := containerName(ctr)
	result, err := dm.client.ContainerStats(ctx, ctr.ID, mobyclient.ContainerStatsOptions{
		Stream:                false,
		IncludePreviousSample: false,
	})
	if err != nil {
		return err
	}
	defer result.Body.Close()

	var apiStats mobycontainer.StatsResponse
	if decodeErr := json.UnmarshalRead(result.Body, &apiStats, json.MatchCaseInsensitiveNames(true), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true)); decodeErr != nil {
		return decodeErr
	}

	statusText, health := parseDockerStatus(ctr.Status)
	if ctr.Health != nil && ctr.Health.Status != "" {
		if parsedHealth, ok := parseDockerHealthStatus(string(ctr.Health.Status)); ok {
			health = parsedHealth
		}
	} else if dm.IsPodman() {
		if podmanHealth, healthErr := dm.getPodmanContainerHealth(ctx, ctr.ID); healthErr == nil {
			health = podmanHealth
		}
	}

	id := shortContainerID(ctr.ID)
	dm.containerStatsMutex.Lock()
	defer dm.containerStatsMutex.Unlock()
	dm.initializeTrackingLocked()

	stats, initialized := dm.containerStatsMap[id]
	if !initialized {
		stats = &containermodel.Stats{Name: name, Id: id, FullID: ctr.ID, Image: ctr.Image}
		dm.containerStatsMap[id] = stats
	}
	stats.Name = name
	stats.Id = id
	stats.FullID = ctr.ID
	stats.Image = ctr.Image
	stats.Status = statusText
	stats.Health = health
	if len(ctr.Ports) > 0 {
		stats.Ports = convertContainerPortsToString(ctr.Ports)
	}
	stats.Cpu = 0
	stats.Mem = 0
	stats.Bandwidth = [2]uint64{0, 0}
	stats.MemLimitBytes = apiStats.MemoryStats.Limit

	dm.initializeCpuTracking(cacheTimeMs)
	previousContainer, previousSystem := dm.getCpuPreviousValues(cacheTimeMs, id)
	cpuPct := calculateCPUPercentLinux(&apiStats, previousContainer, previousSystem)
	usedMemory, err := calculateMemoryUsage(&apiStats)
	if err != nil {
		return fmt.Errorf("container %s memory: %w", name, err)
	}
	dm.setCpuCurrentValues(cacheTimeMs, id, apiStats.CPUStats.CPUUsage.TotalUsage, apiStats.CPUStats.SystemUsage)
	if err := validateCpuPercentage(cpuPct, name); err != nil {
		return err
	}

	if dm.lastNetworkReadTime[cacheTimeMs] == nil {
		dm.lastNetworkReadTime[cacheTimeMs] = make(map[string]time.Time)
	}
	sentDelta, recvDelta := dm.calculateNetworkStats(id, apiStats.Networks, name, cacheTimeMs)
	dm.lastNetworkReadTime[cacheTimeMs][id] = time.Now()
	updateContainerStatsValues(stats, cpuPct, usedMemory, sentDelta, recvDelta, apiStats.Read)
	return nil
}

func (dm *Manager) SetCollectorKey(key uint16) {
	dm.collectorKey = key
}

// ReseedFromCollector copies collector baselines onto a live key when the
// collector sample is old enough to form a useful delta window.
func (dm *Manager) ReseedFromCollector(cacheTimeMs, collectorKey uint16) {
	if dm == nil || cacheTimeMs == collectorKey {
		return
	}
	dm.containerStatsMutex.Lock()
	defer dm.containerStatsMutex.Unlock()
	dm.initializeTrackingLocked()
	collectorTimes := dm.lastNetworkReadTime[collectorKey]
	if len(collectorTimes) == 0 {
		return
	}
	var collectorAt, liveAt time.Time
	for _, at := range collectorTimes {
		if at.After(collectorAt) {
			collectorAt = at
		}
	}
	for _, at := range dm.lastNetworkReadTime[cacheTimeMs] {
		if at.After(liveAt) {
			liveAt = at
		}
	}
	if !collector.ShouldReseed(liveAt, collectorAt, time.Now()) {
		return
	}
	dm.lastCpuContainer[cacheTimeMs] = maps.Clone(dm.lastCpuContainer[collectorKey])
	dm.lastCpuSystem[cacheTimeMs] = maps.Clone(dm.lastCpuSystem[collectorKey])
	dm.lastNetworkReadTime[cacheTimeMs] = maps.Clone(collectorTimes)
	if tracker := dm.networkSentTrackers[collectorKey]; tracker != nil {
		dm.networkSentTrackers[cacheTimeMs] = tracker.Clone()
	}
	if tracker := dm.networkRecvTrackers[collectorKey]; tracker != nil {
		dm.networkRecvTrackers[cacheTimeMs] = tracker.Clone()
	}
}

// pruneBaselinesLocked drops the CPU and network baselines of containers that
// are no longer listed, across every cache key. The caller holds
// containerStatsMutex.
func (dm *Manager) pruneBaselinesLocked() {
	maps.DeleteFunc(dm.startReset, func(id string, _ struct{}) bool { _, ok := dm.validIds[id]; return !ok })
	for _, byID := range dm.lastCpuContainer {
		maps.DeleteFunc(byID, func(id string, _ uint64) bool { _, ok := dm.validIds[id]; return !ok })
	}
	for _, byID := range dm.lastCpuSystem {
		maps.DeleteFunc(byID, func(id string, _ uint64) bool { _, ok := dm.validIds[id]; return !ok })
	}
	for _, byID := range dm.lastNetworkReadTime {
		maps.DeleteFunc(byID, func(id string, _ time.Time) bool { _, ok := dm.validIds[id]; return !ok })
	}
}

// resetBaselinesOnStart drops a container's CPU and network baselines on the
// first pass that sees it in its first minute ("Up 30 seconds"), so a restart
// under the same ID starts from the new run's counters, and keeps the baselines
// the later passes of that minute take. A second restart inside that minute
// sends the counters backwards, which the CPU and network checks read as zero
// for one pass.
func (dm *Manager) resetBaselinesOnStart(id, status string) {
	dm.containerStatsMutex.Lock()
	defer dm.containerStatsMutex.Unlock()
	if !strings.Contains(status, "second") {
		delete(dm.startReset, id)
		return
	}
	if _, done := dm.startReset[id]; done {
		return
	}
	if dm.startReset == nil {
		dm.startReset = make(map[string]struct{})
	}
	dm.startReset[id] = struct{}{}
	dm.deleteContainerStatsLocked(id)
}

func (dm *Manager) deleteContainerStatsLocked(id string) {
	delete(dm.containerStatsMap, id)
	for cacheTime := range dm.lastCpuContainer {
		delete(dm.lastCpuContainer[cacheTime], id)
	}
	for cacheTime := range dm.lastCpuSystem {
		delete(dm.lastCpuSystem[cacheTime], id)
	}
	for cacheTime := range dm.lastNetworkReadTime {
		delete(dm.lastNetworkReadTime[cacheTime], id)
	}
}

// NewManager creates a native Moby client for Docker or Podman.
func NewManager(ctx context.Context, onPodmanDetected func()) *Manager {
	dockerHost, exists := config.LookupEnv(config.EnvDockerHost)
	if !exists {
		dockerHost = getDockerHost()
	}
	if dockerHost == "" {
		return nil
	}

	parsedHost, err := mobyclient.ParseHostURL(dockerHost)
	if err != nil {
		slog.Error("invalid DOCKER_HOST, Docker monitoring disabled", "host", dockerHost, "error", err)
		return nil
	}
	if parsedHost.Scheme == "https" {
		slog.Error("unsupported DOCKER_HOST https scheme, Docker monitoring disabled", "host", dockerHost)
		return nil
	}
	if parsedHost.Scheme != "unix" && parsedHost.Scheme != "tcp" && parsedHost.Scheme != "http" {
		slog.Error("invalid DOCKER_HOST scheme, Docker monitoring disabled", "scheme", parsedHost.Scheme)
		return nil
	}

	timeout := time.Duration(dockerTimeoutMs) * time.Millisecond
	if value, set := config.LookupEnv(config.EnvDockerTimeout); set {
		timeout, err = time.ParseDuration(value)
		if err != nil {
			slog.Error("invalid DOCKER_TIMEOUT, Docker monitoring disabled", "value", value, "error", err)
			return nil
		}
		slog.Debug("docker timeout configured", "timeout", timeout)
	}

	var excludeContainers []string
	if excludeString, set := config.LookupEnv(config.EnvExcludeContainers); set && excludeString != "" {
		for part := range strings.SplitSeq(excludeString, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				excludeContainers = append(excludeContainers, trimmed)
			}
		}
		slog.Debug("excluded container patterns configured", "patterns", excludeContainers)
	}

	manager := &Manager{
		onPodmanDetected:    onPodmanDetected,
		passTimeout:         statsPassTimeout(timeout),
		sem:                 make(chan struct{}, 5),
		containerStatsMap:   make(map[string]*containermodel.Stats),
		excludeContainers:   excludeContainers,
		lastCpuContainer:    make(map[uint16]map[string]uint64),
		lastCpuSystem:       make(map[uint16]map[string]uint64),
		networkSentTrackers: make(map[uint16]*deltatracker.DeltaTracker[string, uint64]),
		networkRecvTrackers: make(map[uint16]*deltatracker.DeltaTracker[string, uint64]),
		lastNetworkReadTime: make(map[uint16]map[string]time.Time),
	}
	manager.client, err = mobyclient.New(
		mobyclient.WithHost(dockerHost),
		mobyclient.WithTimeout(timeout),
		mobyclient.WithUserAgent("Docker-Client/"),
		mobyclient.WithHTTPResponseHook(func(resp *http.Response) {
			if resp != nil && detectPodmanFromHeader(resp.Header.Get("Server")) {
				manager.libpodDetected.Store(true)
			}
		}),
	)
	if err != nil {
		slog.Error("invalid Docker client configuration, Docker monitoring disabled", "host", dockerHost, "error", err)
		return nil
	}

	// Best-effort startup probe. If the engine is not ready, GetStats retries
	// after the first successful container-list request.
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, _ = manager.checkDockerVersion(probeCtx)
	return manager
}

// dockerSockets lists where the daemon looks for an engine when DOCKER_HOST is
// unset, in order: Docker's socket, the Podman service root runs
// (podman.socket), and the Podman of the user the daemon runs as.
func dockerSockets() []string {
	return []string{"/var/run/docker.sock", "/run/podman/podman.sock", fmt.Sprintf("/run/user/%v/podman/podman.sock", os.Getuid())}
}

func getDockerHost() string {
	socks := dockerSockets()
	for _, socket := range socks {
		if _, err := os.Stat(socket); err == nil {
			return "unix://" + socket
		}
	}
	return "unix://" + socks[0]
}

// GetHostInfo fetches native Docker/Podman system information.
func (dm *Manager) GetHostInfo(ctx context.Context) (info mobysystem.Info, err error) {
	if dm == nil || dm.client == nil {
		return info, nil
	}
	result, err := dm.client.Info(ctx, mobyclient.InfoOptions{})
	if err != nil {
		return info, err
	}
	return result.Info, nil
}

func (dm *Manager) IsPodman() bool {
	if dm == nil {
		return false
	}
	return dm.usingPodman.Load()
}

func (dm *Manager) syncPodmanDetection() {
	if dm != nil && dm.libpodDetected.Load() {
		dm.setIsPodman()
	}
}

func (dm *Manager) setIsPodman() {
	if dm.usingPodman.Swap(true) {
		return
	}
	dm.goodDockerVersion = true
	dm.dockerVersionChecked = true
	if dm.onPodmanDetected != nil {
		dm.onPodmanDetected()
	}
}

func (dm *Manager) checkDockerVersion(ctx context.Context) (bool, error) {
	if dm == nil || dm.client == nil {
		return false, errDockerClientUnavailable
	}
	result, err := dm.client.ServerVersion(ctx, mobyclient.ServerVersionOptions{})
	if err != nil {
		return false, err
	}
	dm.syncPodmanDetection()
	switch {
	case dm.libpodDetected.Load() || detectPodmanFromVersion(&result):
		dm.setIsPodman()
	case dockerMajorVersion(result.Version) > 24:
		dm.goodDockerVersion = true
	default:
		slog.Warn("Docker version is outdated; upgrade if possible", "version", result.Version)
	}
	dm.dockerVersionChecked = true
	return true, nil
}

func (dm *Manager) ensureDockerVersionChecked(ctx context.Context) {
	if dm.dockerVersionChecked || ctx.Err() != nil {
		return
	}
	if _, err := dm.checkDockerVersion(ctx); err != nil {
		slog.Debug("Docker version lookup failed", "error", err)
	}
}

func dockerMajorVersion(version string) uint64 {
	major, rest, found := strings.Cut(version, ".")
	if !found {
		return 0
	}
	minor, patchAndSuffix, found := strings.Cut(rest, ".")
	if !found {
		return 0
	}
	patch, _, _ := strings.Cut(patchAndSuffix, "-")
	patch, _, _ = strings.Cut(patch, "+")
	v, err := strconv.ParseUint(major, 10, 64)
	if err != nil {
		return 0
	}
	if _, err := strconv.ParseUint(minor, 10, 64); err != nil {
		return 0
	}
	if _, err := strconv.ParseUint(patch, 10, 64); err != nil {
		return 0
	}
	return v
}

func detectPodmanFromHeader(server string) bool {
	return strings.HasPrefix(server, "Libpod")
}

func detectPodmanFromVersion(versionInfo *mobyclient.ServerVersionResult) bool {
	if versionInfo == nil {
		return false
	}
	for _, component := range versionInfo.Components {
		if strings.HasPrefix(component.Name, "Podman") {
			return true
		}
	}
	return false
}
