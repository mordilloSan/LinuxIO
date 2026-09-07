// Package docker integrates with Docker and Podman Engine APIs.
package docker

import (
	"context"
	"encoding/json"
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

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/deltatracker"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/utils"
)

const (
	dockerTimeoutMs           = 2100
	maxNetworkSpeedBps uint64 = 5e9
	maxMemoryUsage     uint64 = 100 * 1024 * 1024 * 1024 * 1024
	reseedMinWindow           = time.Second
)

var errDockerClientUnavailable = errors.New("docker client unavailable")

type Manager struct {
	onPodmanDetected     func()
	client               *mobyclient.Client
	wg                   sync.WaitGroup
	sem                  chan struct{}
	containerStatsMutex  sync.RWMutex
	containerStatsMap    map[string]*container.Stats
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
// delta tracking.
//
//nolint:gocognit // Container stats collection coordinates API probing, concurrency, and per-container error handling.
func (dm *Manager) GetStats(ctx context.Context, cacheTimeMs uint16) ([]*container.Stats, error) {
	if dm == nil || dm.client == nil {
		return nil, errDockerClientUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dm.initializeTracking()

	list, err := dm.client.ContainerList(ctx, mobyclient.ContainerListOptions{})
	if err != nil {
		return nil, err
	}
	dm.syncPodmanDetection()
	dm.ensureDockerVersionChecked(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	dm.ReseedFromCollector(cacheTimeMs, dm.collectorKey)
	containersLength := len(list.Items)
	dm.validIds = make(map[string]struct{}, containersLength)
	var failedContainers []*mobycontainer.Summary

	for i := range list.Items {
		if err := ctx.Err(); err != nil {
			dm.wg.Wait()
			return nil, err
		}
		ctr := list.Items[i]
		id := shortContainerID(ctr.ID)
		name := containerName(&ctr)
		if dm.shouldExcludeContainer(name) {
			slog.Debug("Excluding container", "name", name)
			continue
		}

		dm.validIds[id] = struct{}{}
		// Created timestamps do not change on restart, so retain status-based
		// restart detection.
		if strings.Contains(ctr.Status, "second") {
			dm.deleteContainerStatsSync(id)
		}
		if !dm.queue(ctx) {
			dm.wg.Wait()
			return nil, ctx.Err()
		}
		go func(ctr mobycontainer.Summary) {
			defer dm.dequeue()
			pprof.Do(ctx, pprof.Labels("component", "docker", "container", shortContainerID(ctr.ID)), func(ctx context.Context) {
				if err := dm.updateContainerStats(ctx, &ctr, cacheTimeMs); err != nil {
					dm.containerStatsMutex.Lock()
					delete(dm.containerStatsMap, shortContainerID(ctr.ID))
					failedContainers = append(failedContainers, &ctr)
					dm.containerStatsMutex.Unlock()
				}
			})
		}(ctr)
	}

	dm.wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Legacy Docker versions require stats requests to be run in one batch.
	// Retrying failed containers separately preserves the existing behavior.
	if len(failedContainers) > 0 {
		slog.Debug("Retrying failed containers", "count", len(failedContainers))
		for _, ctr := range failedContainers {
			if err := ctx.Err(); err != nil {
				dm.wg.Wait()
				return nil, err
			}
			if !dm.queue(ctx) {
				dm.wg.Wait()
				return nil, ctx.Err()
			}
			go func(ctr mobycontainer.Summary) {
				defer dm.dequeue()
				pprof.Do(ctx, pprof.Labels("component", "docker", "container", shortContainerID(ctr.ID), "retry", "true"), func(ctx context.Context) {
					if err := dm.updateContainerStats(ctx, &ctr, cacheTimeMs); err != nil {
						slog.Error("Error getting container stats", "err", err)
					}
				})
			}(*ctr)
		}
		dm.wg.Wait()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}

	stats := make([]*container.Stats, 0, containersLength)
	dm.containerStatsMutex.Lock()
	for id, value := range dm.containerStatsMap {
		if _, exists := dm.validIds[id]; !exists {
			delete(dm.containerStatsMap, id)
			continue
		}
		stats = append(stats, value)
	}
	dm.containerStatsMutex.Unlock()

	dm.cycleNetworkDeltasForCacheTime(cacheTimeMs)
	return stats, nil
}

// GetContainerIdentities returns the running containers known to the runtime.
func (dm *Manager) GetContainerIdentities(ctx context.Context) ([]container.Identity, error) {
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

	identities := make([]container.Identity, 0, len(result.Items))
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
		identities = append(identities, container.Identity{
			ID:     shortContainerID(fullID),
			FullID: fullID,
			Name:   name,
		})
	}
	return identities, nil
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
		dm.containerStatsMap = make(map[string]*container.Stats)
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
		slog.Warn("Bad network delta", "container", name)
		sentDelta = 0
	}
	if recvDelta > maxNetworkSpeedBps {
		slog.Warn("Bad network delta", "container", name)
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

func updateContainerStatsValues(stats *container.Stats, cpuPct float64, usedMemory uint64, sentDelta, recvDelta uint64, readTime time.Time) {
	stats.Cpu = utils.TwoDecimals(cpuPct)
	stats.Mem = utils.BytesToMegabytes(float64(usedMemory))
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

func parseDockerStatus(status string) (string, container.DockerHealth) {
	trimmed := strings.TrimSpace(status)
	if trimmed == "" {
		return "", container.DockerHealthNone
	}
	trimmed = strings.Replace(trimmed, "About ", "", 1)

	statusText, healthText, found := strings.CutLast(trimmed, "(")
	if !found || !strings.HasSuffix(trimmed, ")") {
		return trimmed, container.DockerHealthNone
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
	return trimmed, container.DockerHealthNone
}

func parseDockerHealthStatus(status string) (container.DockerHealth, bool) {
	health, ok := container.DockerHealthStrings[strings.ToLower(strings.TrimSpace(status))]
	return health, ok
}

func (dm *Manager) getPodmanContainerHealth(ctx context.Context, containerID string) (container.DockerHealth, error) {
	if dm == nil || dm.client == nil {
		return container.DockerHealthNone, errDockerClientUnavailable
	}
	result, err := dm.client.ContainerInspect(ctx, containerID, mobyclient.ContainerInspectOptions{})
	if err != nil {
		return container.DockerHealthNone, err
	}
	if result.Container.State == nil || result.Container.State.Health == nil {
		return container.DockerHealthNone, nil
	}
	health, ok := parseDockerHealthStatus(string(result.Container.State.Health.Status))
	if !ok {
		return container.DockerHealthNone, nil
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
	if decodeErr := json.NewDecoder(result.Body).Decode(&apiStats); decodeErr != nil {
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
		stats = &container.Stats{Name: name, Id: id, FullID: ctr.ID, Image: ctr.Image}
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
	if !liveAt.Before(collectorAt) || time.Since(collectorAt) < reseedMinWindow {
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

func (dm *Manager) deleteContainerStatsSync(id string) {
	dm.containerStatsMutex.Lock()
	defer dm.containerStatsMutex.Unlock()
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
	dockerHost, exists := utils.GetEnv("DOCKER_HOST")
	if !exists {
		dockerHost = getDockerHost()
	}
	if dockerHost == "" {
		return nil
	}

	parsedHost, err := mobyclient.ParseHostURL(dockerHost)
	if err != nil {
		slog.Error("Invalid DOCKER_HOST; Docker monitoring disabled", "host", dockerHost, "err", err)
		return nil
	}
	if parsedHost.Scheme == "https" {
		slog.Error("DOCKER_HOST https is not supported; Docker monitoring disabled", "host", dockerHost)
		return nil
	}
	if parsedHost.Scheme != "unix" && parsedHost.Scheme != "tcp" && parsedHost.Scheme != "http" {
		slog.Error("Invalid DOCKER_HOST scheme; Docker monitoring disabled", "scheme", parsedHost.Scheme)
		return nil
	}

	timeout := time.Duration(dockerTimeoutMs) * time.Millisecond
	if value, set := utils.GetEnv("DOCKER_TIMEOUT"); set {
		timeout, err = time.ParseDuration(value)
		if err != nil {
			slog.Error("Invalid DOCKER_TIMEOUT; Docker monitoring disabled", "value", value, "err", err)
			return nil
		}
		slog.Info("DOCKER_TIMEOUT", "timeout", timeout)
	}

	var excludeContainers []string
	if excludeString, set := utils.GetEnv("EXCLUDE_CONTAINERS"); set && excludeString != "" {
		for part := range strings.SplitSeq(excludeString, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				excludeContainers = append(excludeContainers, trimmed)
			}
		}
		slog.Info("EXCLUDE_CONTAINERS", "patterns", excludeContainers)
	}

	manager := &Manager{
		onPodmanDetected:    onPodmanDetected,
		sem:                 make(chan struct{}, 5),
		containerStatsMap:   make(map[string]*container.Stats),
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
		mobyclient.WithResponseHook(func(resp *http.Response) {
			if resp != nil && detectPodmanFromHeader(resp.Header.Get("Server")) {
				manager.libpodDetected.Store(true)
			}
		}),
	)
	if err != nil {
		slog.Error("Invalid Docker client configuration; Docker monitoring disabled", "host", dockerHost, "err", err)
		return nil
	}

	// Best-effort startup probe. If the engine is not ready, GetStats retries
	// after the first successful container-list request.
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, _ = manager.checkDockerVersion(probeCtx)
	return manager
}

func getDockerHost() string {
	socks := []string{"/var/run/docker.sock", fmt.Sprintf("/run/user/%v/podman/podman.sock", os.Getuid())}
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
		slog.Info("Docker version is outdated; upgrade if possible", "version", result.Version)
	}
	dm.dockerVersionChecked = true
	return true, nil
}

func (dm *Manager) ensureDockerVersionChecked(ctx context.Context) {
	if dm.dockerVersionChecked || ctx.Err() != nil {
		return
	}
	if _, err := dm.checkDockerVersion(ctx); err != nil {
		slog.Debug("Failed to get Docker version", "err", err)
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
