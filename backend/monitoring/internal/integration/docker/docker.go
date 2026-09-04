// Package docker integrates with Docker and Podman Engine APIs.
package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/deltatracker"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/integration/docker/dockerapi"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/utils"
)

const (
	// Docker API timeout in milliseconds
	dockerTimeoutMs = 2100
	// Maximum realistic network speed (5 GB/s) to detect bad deltas
	maxNetworkSpeedBps uint64 = 5e9
	// Maximum conceivable memory usage of a container (100TB) to detect bad memory stats
	maxMemoryUsage uint64 = 100 * 1024 * 1024 * 1024 * 1024
)

type Manager struct {
	onPodmanDetected     func()                      // Propagates runtime detail changes back to the app
	client               *http.Client                // Client to query Docker API
	wg                   sync.WaitGroup              // WaitGroup to wait for all goroutines to finish
	sem                  chan struct{}               // Semaphore to limit concurrent container requests
	containerStatsMutex  sync.RWMutex                // Mutex to prevent concurrent access to containerStatsMap
	apiContainerList     []*dockerapi.Info           // List of containers from Docker API
	containerStatsMap    map[string]*container.Stats // Keeps track of container stats
	validIds             map[string]struct{}         // Map of valid container ids, used to prune invalid containers from containerStatsMap
	goodDockerVersion    bool                        // Whether docker version is at least 25.0.0 (one-shot works correctly)
	dockerVersionChecked bool                        // Whether a version probe has completed successfully
	buf                  *bytes.Buffer               // Buffer to store and read response bodies
	decoder              *json.Decoder               // Reusable JSON decoder that reads from buf
	apiStats             *dockerapi.Stats            // Reusable API stats object
	excludeContainers    []string                    // Patterns to exclude containers by name
	usingPodman          bool                        // Whether the Docker Engine API is running on Podman

	// Cache-time-aware tracking for CPU stats (similar to cpu.go)
	// Maps cache time intervals to container-specific CPU usage tracking
	lastCpuContainer map[uint16]map[string]uint64 // cacheTimeMs -> containerId -> last cpu container usage
	lastCpuSystem    map[uint16]map[string]uint64 // cacheTimeMs -> containerId -> last cpu system usage

	// Network delta trackers - one per cache time to avoid interference
	// cacheTimeMs -> DeltaTracker for network bytes sent/received
	networkSentTrackers map[uint16]*deltatracker.DeltaTracker[string, uint64]
	networkRecvTrackers map[uint16]*deltatracker.DeltaTracker[string, uint64]
	lastNetworkReadTime map[uint16]map[string]time.Time // cacheTimeMs -> containerId -> last network read time

	// collectorKey names the cache time the collector owns, set once by the app
	// right after construction so live keys can reseed from its baselines.
	collectorKey uint16
}

// userAgentRoundTripper is a custom http.RoundTripper that adds a User-Agent header to all requests
type userAgentRoundTripper struct {
	rt        http.RoundTripper
	userAgent string
}

// dockerVersionResponse contains the /version fields used for engine checks.
type dockerVersionResponse struct {
	Version    string `json:"Version"`
	Components []struct {
		Name string `json:"Name"`
	} `json:"Components"`
}

// RoundTrip implements the http.RoundTripper interface
func (u *userAgentRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", u.userAgent)
	return u.rt.RoundTrip(req)
}

// Add goroutine to the queue
func (d *Manager) queue(ctx context.Context) bool {
	d.wg.Add(1)
	if d.goodDockerVersion {
		select {
		case d.sem <- struct{}{}:
		case <-ctx.Done():
			d.wg.Done()
			return false
		}
	}
	return true
}

// Remove goroutine from the queue
func (d *Manager) dequeue() {
	d.wg.Done()
	if d.goodDockerVersion {
		<-d.sem
	}
}

// shouldExcludeContainer checks if a container name matches any exclusion pattern
func (dm *Manager) shouldExcludeContainer(name string) bool {
	if len(dm.excludeContainers) == 0 {
		return false
	}
	for _, pattern := range dm.excludeContainers {
		if match, _ := path.Match(pattern, name); match {
			return true
		}
	}
	return false
}

// Returns stats for all running containers with cache-time-aware delta tracking
//
//nolint:gocognit // Container stats collection coordinates API probing, concurrency, and per-container error handling.
func (dm *Manager) GetStats(ctx context.Context, cacheTimeMs uint16) ([]*container.Stats, error) {
	resp, err := dm.get(ctx, "http://localhost/containers/json")
	if err != nil {
		return nil, err
	}

	dm.apiContainerList = dm.apiContainerList[:0]
	if err := dm.decode(resp, &dm.apiContainerList); err != nil {
		return nil, err
	}

	// Detect Podman from Server header
	serverHeader := resp.Header.Get("Server")
	if !dm.usingPodman && detectPodmanFromHeader(serverHeader) {
		dm.setIsPodman()
	}

	dm.ensureDockerVersionChecked(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Reseed before any updateContainerStats goroutine runs. containerStatsMutex
	// is not held here, and ReseedFromCollector takes it itself.
	dm.ReseedFromCollector(cacheTimeMs, dm.collectorKey)

	containersLength := len(dm.apiContainerList)

	// store valid ids to clean up old container ids from map
	if dm.validIds == nil {
		dm.validIds = make(map[string]struct{}, containersLength)
	} else {
		clear(dm.validIds)
	}

	var failedContainers []*dockerapi.Info

	for _, ctr := range dm.apiContainerList {
		if err := ctx.Err(); err != nil {
			dm.wg.Wait()
			return nil, err
		}
		ctr.IdShort = shortContainerID(ctr.Id)

		// Skip this container if it matches the exclusion pattern
		if dm.shouldExcludeContainer(ctr.Names[0][1:]) {
			slog.Debug("Excluding container", "name", ctr.Names[0][1:])
			continue
		}

		dm.validIds[ctr.IdShort] = struct{}{}
		// check if container is less than 1 minute old (possible restart)
		// note: can't use Created field because it's not updated on restart
		if strings.Contains(ctr.Status, "second") {
			// if so, remove old container data
			dm.deleteContainerStatsSync(ctr.IdShort)
		}
		if !dm.queue(ctx) {
			dm.wg.Wait()
			return nil, ctx.Err()
		}
		go func(ctr *dockerapi.Info) {
			defer dm.dequeue()
			// Labelled so a stats call that never returns names its container
			// in the traceback and in the goroutineleak profile.
			pprof.Do(ctx, pprof.Labels("component", "docker", "container", ctr.IdShort), func(ctx context.Context) {
				err := dm.updateContainerStats(ctx, ctr, cacheTimeMs)
				// if error, delete from map and add to failed list to retry
				if err != nil {
					dm.containerStatsMutex.Lock()
					delete(dm.containerStatsMap, ctr.IdShort)
					failedContainers = append(failedContainers, ctr)
					dm.containerStatsMutex.Unlock()
				}
			})
		}(ctr)
	}

	dm.wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// retry failed containers separately so we can run them in parallel (docker 24 bug)
	if len(failedContainers) > 0 {
		slog.Debug("Retrying failed containers", "count", len(failedContainers))
		for i := range failedContainers {
			if err := ctx.Err(); err != nil {
				dm.wg.Wait()
				return nil, err
			}
			ctr := failedContainers[i]
			if !dm.queue(ctx) {
				dm.wg.Wait()
				return nil, ctx.Err()
			}
			go func(ctr *dockerapi.Info) {
				defer dm.dequeue()
				pprof.Do(ctx, pprof.Labels("component", "docker", "container", ctr.IdShort, "retry", "true"), func(ctx context.Context) {
					if err2 := dm.updateContainerStats(ctx, ctr, cacheTimeMs); err2 != nil {
						slog.Error("Error getting container stats", "err", err2)
					}
				})
			}(ctr)
		}
		dm.wg.Wait()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}

	// populate final stats and remove old / invalid container stats
	stats := make([]*container.Stats, 0, containersLength)
	for id, v := range dm.containerStatsMap {
		if _, exists := dm.validIds[id]; !exists {
			delete(dm.containerStatsMap, id)
		} else {
			stats = append(stats, v)
		}
	}

	// prepare network trackers for next interval for this cache time
	dm.cycleNetworkDeltasForCacheTime(cacheTimeMs)

	return stats, nil
}

// GetContainerIdentities returns the running containers known to the runtime.
// It intentionally uses /containers/json only: process attribution callers can
// refresh identities without paying for a stats request per container.
func (dm *Manager) GetContainerIdentities(ctx context.Context) ([]container.Identity, error) {
	if dm == nil {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resp, err := dm.get(ctx, "http://localhost/containers/json?all=false")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("container list request failed: %s", resp.Status)
	}
	if !dm.usingPodman && detectPodmanFromHeader(resp.Header.Get("Server")) {
		dm.setIsPodman()
	}
	var entries []dockerapi.Info
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	identities := make([]container.Identity, 0, len(entries))
	for i := range entries {
		entry := &entries[i]
		// all=false is the runtime-side filter; retain this guard for
		// compatible engines that ignore the query or return stale entries.
		if state := strings.TrimSpace(entry.State); state != "" && !strings.EqualFold(state, "running") {
			continue
		}
		name := ""
		if len(entry.Names) > 0 {
			name = strings.TrimPrefix(strings.TrimSpace(entry.Names[0]), "/")
		}
		if dm.shouldExcludeContainer(name) {
			continue
		}
		fullID := strings.TrimSpace(entry.Id)
		if fullID == "" {
			continue
		}
		identities = append(identities, container.Identity{
			ID:     shortContainerID(fullID),
			FullID: fullID,
			Name:   name,
		})
	}
	return identities, nil
}

func shortContainerID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// initializeCpuTracking initializes CPU tracking maps for a specific cache time interval
func (dm *Manager) initializeCpuTracking(cacheTimeMs uint16) {
	// Initialize cache time maps if they don't exist
	if dm.lastCpuContainer[cacheTimeMs] == nil {
		dm.lastCpuContainer[cacheTimeMs] = make(map[string]uint64)
	}
	if dm.lastCpuSystem[cacheTimeMs] == nil {
		dm.lastCpuSystem[cacheTimeMs] = make(map[string]uint64)
	}
}

// getCpuPreviousValues returns previous CPU values for a container and cache time interval
func (dm *Manager) getCpuPreviousValues(cacheTimeMs uint16, containerId string) (uint64, uint64) {
	return dm.lastCpuContainer[cacheTimeMs][containerId], dm.lastCpuSystem[cacheTimeMs][containerId]
}

// setCpuCurrentValues stores current CPU values for a container and cache time interval
func (dm *Manager) setCpuCurrentValues(cacheTimeMs uint16, containerId string, cpuContainer, cpuSystem uint64) {
	dm.lastCpuContainer[cacheTimeMs][containerId] = cpuContainer
	dm.lastCpuSystem[cacheTimeMs][containerId] = cpuSystem
}

// calculateMemoryUsage calculates memory usage from Docker API stats
func calculateMemoryUsage(apiStats *dockerapi.Stats) (uint64, error) {
	memCache := apiStats.MemoryStats.Stats.InactiveFile
	if memCache == 0 {
		memCache = apiStats.MemoryStats.Stats.Cache
	}

	usedDelta := apiStats.MemoryStats.Usage - memCache
	if usedDelta <= 0 || usedDelta > maxMemoryUsage {
		return 0, fmt.Errorf("bad memory stats")
	}

	return usedDelta, nil
}

// getNetworkTracker returns the DeltaTracker for a specific cache time, creating it if needed
func (dm *Manager) getNetworkTracker(cacheTimeMs uint16, isSent bool) *deltatracker.DeltaTracker[string, uint64] {
	var trackers map[uint16]*deltatracker.DeltaTracker[string, uint64]
	if isSent {
		trackers = dm.networkSentTrackers
	} else {
		trackers = dm.networkRecvTrackers
	}

	if trackers[cacheTimeMs] == nil {
		trackers[cacheTimeMs] = deltatracker.NewDeltaTracker[string, uint64]()
	}

	return trackers[cacheTimeMs]
}

// cycleNetworkDeltasForCacheTime cycles the network delta trackers for a specific cache time
func (dm *Manager) cycleNetworkDeltasForCacheTime(cacheTimeMs uint16) {
	if dm.networkSentTrackers[cacheTimeMs] != nil {
		dm.networkSentTrackers[cacheTimeMs].Cycle()
	}
	if dm.networkRecvTrackers[cacheTimeMs] != nil {
		dm.networkRecvTrackers[cacheTimeMs].Cycle()
	}
}

// calculateNetworkStats calculates network sent/receive deltas using DeltaTracker
func (dm *Manager) calculateNetworkStats(ctr *dockerapi.Info, apiStats *dockerapi.Stats, name string, cacheTimeMs uint16) (uint64, uint64) {
	var total_sent, total_recv uint64
	for _, v := range apiStats.Networks {
		total_sent += v.TxBytes
		total_recv += v.RxBytes
	}

	// Get the DeltaTracker for this specific cache time
	sentTracker := dm.getNetworkTracker(cacheTimeMs, true)
	recvTracker := dm.getNetworkTracker(cacheTimeMs, false)

	// Set current values in the cache-time-specific DeltaTracker
	sentTracker.Set(ctr.IdShort, total_sent)
	recvTracker.Set(ctr.IdShort, total_recv)

	// Get deltas (bytes since last measurement)
	sent_delta_raw := sentTracker.Delta(ctr.IdShort)
	recv_delta_raw := recvTracker.Delta(ctr.IdShort)

	// Calculate bytes per second using per-cache-time read time to avoid
	// interference between different cache intervals (e.g. 1000ms vs 60000ms)
	var sent_delta, recv_delta uint64
	if prevReadTime, ok := dm.lastNetworkReadTime[cacheTimeMs][ctr.IdShort]; ok {
		millisecondsElapsed := uint64(time.Since(prevReadTime).Milliseconds())
		if millisecondsElapsed > 0 {
			if sent_delta_raw > 0 {
				sent_delta = sent_delta_raw * 1000 / millisecondsElapsed
				if sent_delta > maxNetworkSpeedBps {
					slog.Warn("Bad network delta", "container", name)
					sent_delta = 0
				}
			}
			if recv_delta_raw > 0 {
				recv_delta = recv_delta_raw * 1000 / millisecondsElapsed
				if recv_delta > maxNetworkSpeedBps {
					slog.Warn("Bad network delta", "container", name)
					recv_delta = 0
				}
			}
		}
	}

	return sent_delta, recv_delta
}

// validateCpuPercentage checks if CPU percentage is within valid range
func validateCpuPercentage(cpuPct float64, containerName string) error {
	if cpuPct > 100 {
		return fmt.Errorf("%s cpu pct greater than 100: %+v", containerName, cpuPct)
	}
	return nil
}

// updateContainerStatsValues updates the final stats values
func updateContainerStatsValues(stats *container.Stats, cpuPct float64, usedMemory uint64, sent_delta, recv_delta uint64, readTime time.Time) {
	stats.Cpu = utils.TwoDecimals(cpuPct)
	stats.Mem = utils.BytesToMegabytes(float64(usedMemory))
	stats.Bandwidth = [2]uint64{sent_delta, recv_delta}
	stats.PrevReadTime = readTime
}

// convertContainerPortsToString formats the ports of a container into a sorted, deduplicated string.
// ctr.Ports is nilled out after processing so the slice is not accidentally reused.
func convertContainerPortsToString(ctr *dockerapi.Info) string {
	if len(ctr.Ports) == 0 {
		return ""
	}
	sort.Slice(ctr.Ports, func(i, j int) bool {
		return ctr.Ports[i].PublicPort < ctr.Ports[j].PublicPort
	})
	var builder strings.Builder
	seenPorts := make(map[uint16]struct{})
	for _, p := range ctr.Ports {
		_, ok := seenPorts[p.PublicPort]
		if p.PublicPort == 0 || ok {
			continue
		}
		seenPorts[p.PublicPort] = struct{}{}
		if builder.Len() > 0 {
			builder.WriteString(", ")
		}
		switch p.IP {
		case "0.0.0.0", "::":
		default:
			builder.WriteString(p.IP)
			builder.WriteByte(':')
		}
		builder.WriteString(strconv.Itoa(int(p.PublicPort)))
	}
	// clear ports slice so it doesn't get reused and blend into next response
	ctr.Ports = nil
	return builder.String()
}

func parseDockerStatus(status string) (string, container.DockerHealth) {
	trimmed := strings.TrimSpace(status)
	if trimmed == "" {
		return "", container.DockerHealthNone
	}

	// Remove "About " from status
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
	// Some Docker statuses include a "health:" prefix inside the parentheses.
	// Strip it so it maps correctly to the known health states.
	if colonIdx := strings.IndexRune(healthText, ':'); colonIdx != -1 {
		prefix := strings.ToLower(strings.TrimSpace(healthText[:colonIdx]))
		if prefix == "health" || prefix == "health status" {
			healthText = strings.TrimSpace(healthText[colonIdx+1:])
		}
	}
	if health, ok := parseDockerHealthStatus(healthText); ok {
		return statusText, health
	}

	return trimmed, container.DockerHealthNone
}

// parseDockerHealthStatus maps Docker health status strings to container.DockerHealth values
func parseDockerHealthStatus(status string) (container.DockerHealth, bool) {
	health, ok := container.DockerHealthStrings[strings.ToLower(strings.TrimSpace(status))]
	return health, ok
}

// getPodmanContainerHealth fetches container health status from the container inspect endpoint.
// Used for Podman which doesn't provide health status in the /containers/json endpoint as of March 2026.
// https://github.com/containers/podman/issues/27786
func (dm *Manager) getPodmanContainerHealth(ctx context.Context, containerID string) (container.DockerHealth, error) {
	resp, err := dm.get(ctx, fmt.Sprintf("http://localhost/containers/%s/json", url.PathEscape(containerID)))
	if err != nil {
		return container.DockerHealthNone, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return container.DockerHealthNone, fmt.Errorf("container inspect request failed: %s", resp.Status)
	}

	var inspectInfo struct {
		State struct {
			Health struct {
				Status string
			}
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(&inspectInfo); err != nil {
		return container.DockerHealthNone, err
	}

	if health, ok := parseDockerHealthStatus(inspectInfo.State.Health.Status); ok {
		return health, nil
	}

	return container.DockerHealthNone, nil
}

// Updates stats for individual container with cache-time-aware delta tracking
func (dm *Manager) updateContainerStats(ctx context.Context, ctr *dockerapi.Info, cacheTimeMs uint16) error {
	name := ctr.Names[0][1:]

	resp, err := dm.get(ctx, fmt.Sprintf("http://localhost/containers/%s/stats?stream=0&one-shot=1", ctr.IdShort))
	if err != nil {
		return err
	}

	statusText, health := parseDockerStatus(ctr.Status)

	// Docker exposes Health.Status on /containers/json in API 1.52+.
	// Podman currently requires falling back to the inspect endpoint as of March 2026.
	// https://github.com/containers/podman/issues/27786
	if ctr.Health.Status != "" {
		if h, ok := parseDockerHealthStatus(ctr.Health.Status); ok {
			health = h
		}
	} else if dm.usingPodman {
		if podmanHealth, healthErr := dm.getPodmanContainerHealth(ctx, ctr.IdShort); healthErr == nil {
			health = podmanHealth
		}
	}

	dm.containerStatsMutex.Lock()
	defer dm.containerStatsMutex.Unlock()

	// add empty values if they doesn't exist in map
	stats, initialized := dm.containerStatsMap[ctr.IdShort]
	if !initialized {
		stats = &container.Stats{Name: name, Id: ctr.IdShort, FullID: ctr.Id, Image: ctr.Image}
		dm.containerStatsMap[ctr.IdShort] = stats
	}

	stats.Id = ctr.IdShort
	stats.FullID = ctr.Id
	stats.Status = statusText
	stats.Health = health

	if len(ctr.Ports) > 0 {
		stats.Ports = convertContainerPortsToString(ctr)
	}

	// reset current stats
	stats.Cpu = 0
	stats.Mem = 0
	stats.Bandwidth = [2]uint64{0, 0}

	res := dm.apiStats
	res.Networks = nil
	if decodeErr := dm.decode(resp, res); decodeErr != nil {
		return decodeErr
	}

	// Initialize CPU tracking for this cache time interval
	dm.initializeCpuTracking(cacheTimeMs)

	// Get previous CPU values
	prevCpuContainer, prevCpuSystem := dm.getCpuPreviousValues(cacheTimeMs, ctr.IdShort)

	cpuPct := res.CalculateCPUPercentLinux(prevCpuContainer, prevCpuSystem)

	// Calculate memory usage
	usedMemory, err := calculateMemoryUsage(res)
	if err != nil {
		return fmt.Errorf("%s - %w - see https://github.com/mordilloSan/go-monitoring/issues/144", name, err)
	}

	// Store current CPU stats for next calculation
	currentCpuContainer := res.CPUStats.CPUUsage.TotalUsage
	currentCpuSystem := res.CPUStats.SystemUsage
	dm.setCpuCurrentValues(cacheTimeMs, ctr.IdShort, currentCpuContainer, currentCpuSystem)

	// Validate CPU percentage
	if err := validateCpuPercentage(cpuPct, name); err != nil {
		return err
	}

	// Calculate network stats using DeltaTracker
	sent_delta, recv_delta := dm.calculateNetworkStats(ctr, res, name, cacheTimeMs)

	// Store per-cache-time network read time for next rate calculation
	if dm.lastNetworkReadTime[cacheTimeMs] == nil {
		dm.lastNetworkReadTime[cacheTimeMs] = make(map[string]time.Time)
	}
	dm.lastNetworkReadTime[cacheTimeMs][ctr.IdShort] = time.Now()

	// Update final stats values
	updateContainerStatsValues(stats, cpuPct, usedMemory, sent_delta, recv_delta, res.Read)

	return nil
}

// SetCollectorKey records the cache time the collector uses. It is called once
// after construction, before any GetStats request.
func (dm *Manager) SetCollectorKey(key uint16) {
	dm.collectorKey = key
}

// reseedMinWindow mirrors app.reseedMinWindow: a collector baseline younger
// than this leaves a delta window of milliseconds, which reads as an amplified
// rate. The app package cannot be imported here without an import cycle.
const reseedMinWindow = time.Second

// ReseedFromCollector copies the collector's per-container baselines onto a
// live key whose newest read predates the collector's newest read, provided
// that read is at least reseedMinWindow old.
func (dm *Manager) ReseedFromCollector(cacheTimeMs, collectorKey uint16) {
	if cacheTimeMs == collectorKey {
		return
	}
	dm.containerStatsMutex.Lock()
	defer dm.containerStatsMutex.Unlock()
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

// Delete container stats from map using mutex
func (dm *Manager) deleteContainerStatsSync(id string) {
	dm.containerStatsMutex.Lock()
	defer dm.containerStatsMutex.Unlock()
	delete(dm.containerStatsMap, id)
	for ct := range dm.lastCpuContainer {
		delete(dm.lastCpuContainer[ct], id)
	}
	for ct := range dm.lastCpuSystem {
		delete(dm.lastCpuSystem[ct], id)
	}
	for ct := range dm.lastNetworkReadTime {
		delete(dm.lastNetworkReadTime[ct], id)
	}
}

// NewManager creates a new HTTP client for Docker or Podman API.
func NewManager(ctx context.Context, onPodmanDetected func()) *Manager {
	dockerHost, exists := utils.GetEnv("DOCKER_HOST")
	if exists {
		// return nil if set to empty string
		if dockerHost == "" {
			return nil
		}
	} else {
		dockerHost = getDockerHost()
	}

	parsedURL, err := url.Parse(dockerHost)
	if err != nil {
		slog.Error("Invalid DOCKER_HOST; Docker monitoring disabled", "host", dockerHost, "err", err)
		return nil
	}

	transport := &http.Transport{
		DisableCompression: true,
		MaxConnsPerHost:    0,
	}

	switch parsedURL.Scheme {
	case "unix":
		transport.DialContext = func(ctx context.Context, proto, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", parsedURL.Path)
		}
	case "tcp", "http":
		transport.DialContext = func(ctx context.Context, proto, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", parsedURL.Host)
		}
	case "https":
		// Requests are built as plain http://, so a TLS endpoint would receive
		// cleartext. Refuse it instead of silently downgrading.
		slog.Error("DOCKER_HOST https is not supported; Docker monitoring disabled", "host", dockerHost)
		return nil
	default:
		slog.Error("Invalid DOCKER_HOST scheme; Docker monitoring disabled", "scheme", parsedURL.Scheme)
		return nil
	}

	// configurable timeout
	timeout := time.Millisecond * time.Duration(dockerTimeoutMs)
	if t, set := utils.GetEnv("DOCKER_TIMEOUT"); set {
		timeout, err = time.ParseDuration(t)
		if err != nil {
			slog.Error("Invalid DOCKER_TIMEOUT; Docker monitoring disabled", "value", t, "err", err)
			return nil
		}
		slog.Info("DOCKER_TIMEOUT", "timeout", timeout)
	}

	// Custom user-agent to avoid docker bug: https://github.com/docker/for-mac/issues/7575
	userAgentTransport := &userAgentRoundTripper{
		rt:        transport,
		userAgent: "Docker-Client/",
	}

	// Read container exclusion patterns from environment variable
	var excludeContainers []string
	if excludeStr, set := utils.GetEnv("EXCLUDE_CONTAINERS"); set && excludeStr != "" {
		parts := strings.SplitSeq(excludeStr, ",")
		for part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				excludeContainers = append(excludeContainers, trimmed)
			}
		}
		slog.Info("EXCLUDE_CONTAINERS", "patterns", excludeContainers)
	}

	manager := &Manager{
		onPodmanDetected: onPodmanDetected,
		client: &http.Client{
			Timeout:   timeout,
			Transport: userAgentTransport,
		},
		containerStatsMap: make(map[string]*container.Stats),
		sem:               make(chan struct{}, 5),
		apiContainerList:  []*dockerapi.Info{},
		apiStats:          &dockerapi.Stats{},
		excludeContainers: excludeContainers,

		// Initialize cache-time-aware tracking structures
		lastCpuContainer:    make(map[uint16]map[string]uint64),
		lastCpuSystem:       make(map[uint16]map[string]uint64),
		networkSentTrackers: make(map[uint16]*deltatracker.DeltaTracker[string, uint64]),
		networkRecvTrackers: make(map[uint16]*deltatracker.DeltaTracker[string, uint64]),
		lastNetworkReadTime: make(map[uint16]map[string]time.Time),
	}

	// Best-effort startup probe. If the engine is not ready yet, GetStats will
	// retry after the first successful /containers/json request.
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, _ = manager.checkDockerVersion(probeCtx)

	return manager
}

// checkDockerVersion checks Docker version and sets goodDockerVersion if at least 25.0.0.
// Versions before 25.0.0 have a bug with one-shot which requires all requests to be made in one batch.
func (dm *Manager) checkDockerVersion(ctx context.Context) (bool, error) {
	resp, err := dm.get(ctx, "http://localhost/version")
	if err != nil {
		return false, err
	}
	if resp.StatusCode != http.StatusOK {
		status := resp.Status
		resp.Body.Close()
		return false, fmt.Errorf("docker version request failed: %s", status)
	}

	var versionInfo dockerVersionResponse
	serverHeader := resp.Header.Get("Server")
	if err := dm.decode(resp, &versionInfo); err != nil {
		return false, err
	}

	dm.applyDockerVersionInfo(serverHeader, &versionInfo)
	dm.dockerVersionChecked = true
	return true, nil
}

// ensureDockerVersionChecked retries the version probe after a successful
// container list request.
func (dm *Manager) ensureDockerVersionChecked(ctx context.Context) {
	if dm.dockerVersionChecked {
		return
	}
	if ctx.Err() != nil {
		return
	}
	if _, err := dm.checkDockerVersion(ctx); err != nil {
		slog.Debug("Failed to get Docker version", "err", err)
	}
}

// applyDockerVersionInfo updates version-dependent behavior from engine metadata.
func (dm *Manager) applyDockerVersionInfo(serverHeader string, versionInfo *dockerVersionResponse) {
	if detectPodmanEngine(serverHeader, versionInfo) {
		dm.setIsPodman()
		return
	}
	// if version > 24, one-shot works correctly and we can limit concurrent operations
	if dockerMajorVersion(versionInfo.Version) > 24 {
		dm.goodDockerVersion = true
	} else {
		slog.Info(fmt.Sprintf("Docker %s is outdated. Upgrade if possible. See https://github.com/mordilloSan/go-monitoring/issues/58", versionInfo.Version))
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

// Decodes Docker API JSON response using a reusable buffer and decoder. Not thread safe.
func (dm *Manager) decode(resp *http.Response, d any) error {
	if dm.buf == nil {
		// initialize buffer with 256kb starting size
		dm.buf = bytes.NewBuffer(make([]byte, 0, 1024*256))
		dm.decoder = json.NewDecoder(dm.buf)
	}
	defer resp.Body.Close()
	defer dm.buf.Reset()
	_, err := dm.buf.ReadFrom(resp.Body)
	if err != nil {
		return err
	}
	return dm.decoder.Decode(d)
}

// Test docker / podman sockets and return if one exists
func getDockerHost() string {
	scheme := "unix://"
	socks := []string{"/var/run/docker.sock", fmt.Sprintf("/run/user/%v/podman/podman.sock", os.Getuid())}
	for _, sock := range socks {
		if _, err := os.Stat(sock); err == nil {
			return scheme + sock
		}
	}
	return scheme + socks[0]
}

// GetHostInfo fetches the system info from Docker
func (dm *Manager) GetHostInfo(ctx context.Context) (info dockerapi.HostInfo, err error) {
	if dm == nil {
		return info, nil
	}
	resp, err := dm.get(ctx, "http://localhost/info")
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return info, err
	}

	return info, nil
}

func (dm *Manager) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return dm.client.Do(req)
}

func (dm *Manager) IsPodman() bool {
	if dm == nil {
		return false
	}
	return dm.usingPodman
}

// setIsPodman sets the manager to Podman mode and updates system details accordingly.
func (dm *Manager) setIsPodman() {
	if dm.usingPodman {
		return
	}
	dm.usingPodman = true
	dm.goodDockerVersion = true
	dm.dockerVersionChecked = true
	// keep system details updated - this may be detected after startup if the
	// container runtime is not ready yet.
	if dm.onPodmanDetected != nil {
		dm.onPodmanDetected()
	}
}

// detectPodmanFromHeader identifies Podman from the Docker API server header.
func detectPodmanFromHeader(server string) bool {
	return strings.HasPrefix(server, "Libpod")
}

// detectPodmanFromVersion identifies Podman from the version payload.
func detectPodmanFromVersion(versionInfo *dockerVersionResponse) bool {
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

// detectPodmanEngine checks both header and version metadata for Podman.
func detectPodmanEngine(serverHeader string, versionInfo *dockerVersionResponse) bool {
	if detectPodmanFromHeader(serverHeader) {
		return true
	}
	return detectPodmanFromVersion(versionInfo)
}
