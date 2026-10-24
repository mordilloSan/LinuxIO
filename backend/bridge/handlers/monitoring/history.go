package monitoring

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

const (
	maxHistoryPayloadBytes = 8 << 20
	maxHistoryLimit        = 1000
	defaultHistoryLimit    = 100
)

// historyReadTimeout bounds one history read. controlClient stays unbounded so
// long-running db.maintain commands survive, so the deadline lives here.
var historyReadTimeout = 15 * time.Second

// logicalCPUCount is injectable so history conversion tests can use a
// deterministic CPU count while production requests retain gopsutil's
// context-aware host query.
var logicalCPUCount = cpu.CountsWithContext

func FetchCPUHistory(ctx context.Context, req apischema.MonitoringHistoryRequest) ([]apischema.MonitoringCPUHistoryPoint, error) {
	return fetchHistory(ctx, monitoringapi.PluginCPU, req, func(item monitoringapi.HistoryItem, stats monitoringapi.CPUHistoryStats) apischema.MonitoringCPUHistoryPoint {
		return apischema.MonitoringCPUHistoryPoint{
			CapturedAtMs:     item.CapturedAt,
			UsagePercent:     stats.CPUPercent,
			BreakdownPercent: stats.BreakdownPercent,
			CoresPercent:     stats.CoresPercent,
			FrequenciesMHz:   stats.FrequenciesMHz,
		}
	})
}

func FetchMemoryHistory(ctx context.Context, req apischema.MonitoringHistoryRequest) ([]apischema.MonitoringMemoryHistoryPoint, error) {
	points, err := fetchHistory(ctx, monitoringapi.PluginMem, req, func(item monitoringapi.HistoryItem, stats monitoringapi.MemHistoryStats) apischema.MonitoringMemoryHistoryPoint {
		return apischema.MonitoringMemoryHistoryPoint{
			CapturedAtMs:  item.CapturedAt,
			TotalGB:       stats.TotalGB,
			UsedGB:        stats.UsedGB,
			UsedPercent:   stats.UsedPercent,
			BufferCacheGB: stats.BufferCacheGB,
			ZFSArcGB:      stats.ZFSArcGB,
			CachedGB:      stats.CachedGB,
			BuffersGB:     stats.BuffersGB,
		}
	})
	if err != nil {
		return nil, err
	}
	mergeDockerMemHistory(ctx, req, points)
	return points, nil
}

type dockerMemHistoryPoint struct {
	capturedAtMs int64
	usedGB       float64
}

// mergeDockerMemHistory annotates memory points with the summed container
// memory captured nearest to each point, so the memory chart can carve a
// Docker layer out of "used". Best-effort: the containers plugin may have
// history disabled, so failures just leave DockerUsedGB zero.
func mergeDockerMemHistory(ctx context.Context, req apischema.MonitoringHistoryRequest, points []apischema.MonitoringMemoryHistoryPoint) {
	if len(points) == 0 {
		return
	}
	docker, err := fetchHistory(ctx, monitoringapi.PluginContainers, req, func(item monitoringapi.HistoryItem, records []monitoringapi.ContainerHistoryRecord) dockerMemHistoryPoint {
		var memMB float64
		for _, record := range records {
			memMB += record.MemMB
		}
		return dockerMemHistoryPoint{capturedAtMs: item.CapturedAt, usedGB: memMB / 1024}
	})
	if err != nil || len(docker) == 0 {
		return
	}

	dockerTimes := make([]int64, len(docker))
	for i, point := range docker {
		dockerTimes[i] = point.capturedAtMs
	}
	nearest := nearestTimeMatcher(dockerTimes, resolutionStepMs(req.Resolution))
	for i := range points {
		if j := nearest(points[i].CapturedAtMs); j >= 0 {
			points[i].DockerUsedGB = docker[j].usedGB
		}
	}
}

// nearestTimeMatcher returns a lookup over an ascending series of capture
// timestamps. Two plugins snapshot on the same collector tick, but their
// rollup buckets can land a little apart, so a caller walking its own
// ascending points asks for the nearest source sample and gets -1 when none
// falls within tolerance.
func nearestTimeMatcher(times []int64, tolerance int64) func(int64) int {
	j := 0
	return func(t int64) int {
		if len(times) == 0 {
			return -1
		}
		for j+1 < len(times) && absInt64(times[j+1]-t) <= absInt64(times[j]-t) {
			j++
		}
		if absInt64(times[j]-t) <= tolerance {
			return j
		}
		return -1
	}
}

// FetchContainerHistory returns per-container CPU, memory, and network history
// from the agent's containers plugin, annotated with block I/O from its
// container_telemetry plugin where that plugin has a nearby sample.
func FetchContainerHistory(ctx context.Context, req apischema.MonitoringHistoryRequest) ([]apischema.MonitoringContainerHistoryPoint, error) {
	logicalCPUs, err := logicalCPUCount(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("count logical CPUs for container history: %w", err)
	}
	if logicalCPUs <= 0 {
		return nil, fmt.Errorf("count logical CPUs for container history: got %d", logicalCPUs)
	}
	cpuMultiplier := float64(logicalCPUs)

	points, err := fetchHistory(ctx, monitoringapi.PluginContainers, req, func(item monitoringapi.HistoryItem, records []monitoringapi.ContainerHistoryRecord) apischema.MonitoringContainerHistoryPoint {
		samples := make([]apischema.MonitoringContainerSample, 0, len(records))
		for _, record := range records {
			samples = append(samples, apischema.MonitoringContainerSample{
				ID:              record.ID,
				Name:            record.Name,
				CPUPercent:      record.CPUPct * cpuMultiplier,
				MemoryMB:        record.MemMB,
				SentBytesPerSec: float64(record.Bandwidth[0]),
				RecvBytesPerSec: float64(record.Bandwidth[1]),
			})
		}
		return apischema.MonitoringContainerHistoryPoint{CapturedAtMs: item.CapturedAt, Containers: samples}
	})
	if err != nil {
		return nil, err
	}
	mergeContainerDiskHistory(ctx, req, points)
	return points, nil
}

type containerDiskHistoryPoint struct {
	capturedAtMs int64
	byContainer  map[string]monitoringapi.ContainerTelemetryRecord
}

// mergeContainerDiskHistory annotates each container sample with the block I/O
// captured nearest to it. Best-effort: container_telemetry is a separate
// history plugin an operator can disable, so failures leave the disk fields
// nil rather than zero.
func mergeContainerDiskHistory(ctx context.Context, req apischema.MonitoringHistoryRequest, points []apischema.MonitoringContainerHistoryPoint) {
	if len(points) == 0 {
		return
	}
	telemetry, err := fetchHistory(ctx, monitoringapi.PluginContainerTelemetry, req, func(item monitoringapi.HistoryItem, records []monitoringapi.ContainerTelemetryRecord) containerDiskHistoryPoint {
		byContainer := make(map[string]monitoringapi.ContainerTelemetryRecord, len(records))
		for _, record := range records {
			byContainer[containerKey(record.ID, record.Name)] = record
		}
		return containerDiskHistoryPoint{capturedAtMs: item.CapturedAt, byContainer: byContainer}
	})
	if err != nil || len(telemetry) == 0 {
		return
	}

	times := make([]int64, len(telemetry))
	for i, point := range telemetry {
		times[i] = point.capturedAtMs
	}
	nearest := nearestTimeMatcher(times, resolutionStepMs(req.Resolution))
	for i := range points {
		j := nearest(points[i].CapturedAtMs)
		if j < 0 {
			continue
		}
		for k := range points[i].Containers {
			sample := &points[i].Containers[k]
			record, ok := telemetry[j].byContainer[containerKey(sample.ID, sample.Name)]
			if !ok {
				continue
			}
			read := float64(record.DiskReadBytesPerSecond)
			write := float64(record.DiskWriteBytesPerSecond)
			sample.ReadBytesPerSec = &read
			sample.WriteBytesPerSec = &write
		}
	}
}

// containerKey identifies a container across the two plugins. Both report the
// agent's short ID, but a record that lost its ID through an old rollup still
// carries the name.
func containerKey(id, name string) string {
	if id != "" {
		return "id:" + id
	}
	return "name:" + name
}

func resolutionStepMs(resolution apischema.MonitoringHistoryResolution) int64 {
	step, ok := monitoringapi.HistoryResolutionStep(string(resolution))
	if !ok {
		return time.Minute.Milliseconds()
	}
	return step.Milliseconds()
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func FetchDiskIOHistory(ctx context.Context, req apischema.MonitoringHistoryRequest) ([]apischema.MonitoringDiskIOHistoryPoint, error) {
	return fetchHistory(ctx, monitoringapi.PluginDiskIO, req, func(item monitoringapi.HistoryItem, stats monitoringapi.DiskIOHistoryStats) apischema.MonitoringDiskIOHistoryPoint {
		return apischema.MonitoringDiskIOHistoryPoint{
			CapturedAtMs:     item.CapturedAt,
			ReadBytesPerSec:  stats.IOBytesPerSec[0],
			WriteBytesPerSec: stats.IOBytesPerSec[1],
		}
	})
}

func FetchNetworkHistory(ctx context.Context, req apischema.MonitoringHistoryRequest) ([]apischema.MonitoringNetworkHistoryPoint, error) {
	return fetchHistory(ctx, monitoringapi.PluginNetwork, req, func(item monitoringapi.HistoryItem, stats monitoringapi.NetworkHistoryStats) apischema.MonitoringNetworkHistoryPoint {
		point := apischema.MonitoringNetworkHistoryPoint{
			CapturedAtMs:    item.CapturedAt,
			SentBytesPerSec: stats.BandwidthBytesPerSec[0],
			RecvBytesPerSec: stats.BandwidthBytesPerSec[1],
		}
		if len(stats.NetworkInterfaces) > 0 {
			point.Interfaces = make(map[string]apischema.MonitoringNetworkRates, len(stats.NetworkInterfaces))
			for name, rates := range stats.NetworkInterfaces {
				point.Interfaces[name] = apischema.MonitoringNetworkRates{
					SentBytesPerSec: rates[0],
					RecvBytesPerSec: rates[1],
				}
			}
		}
		return point
	})
}

func fetchHistory[S, P any](ctx context.Context, plugin string, req apischema.MonitoringHistoryRequest, flatten func(monitoringapi.HistoryItem, S) P) ([]P, error) {
	envelope, err := fetchHistoryEnvelope(ctx, plugin, req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s history: %w", plugin, err)
	}
	points := make([]P, 0, len(envelope.Items))
	for _, item := range envelope.Items {
		var stats S
		if err := json.Unmarshal(item.Stats, &stats); err != nil {
			return nil, fmt.Errorf("decode %s history point: %w", plugin, err)
		}
		points = append(points, flatten(item, stats))
	}
	return points, nil
}

func fetchHistoryEnvelope(ctx context.Context, plugin string, req apischema.MonitoringHistoryRequest) (monitoringapi.HistoryResponse, error) {
	if _, ok := monitoringapi.HistoryResolutionStep(string(req.Resolution)); !ok {
		return monitoringapi.HistoryResponse{}, fmt.Errorf("%w: invalid resolution %q", bridgeipc.ErrInvalidArgs, req.Resolution)
	}
	if req.Limit < 0 || req.Limit > maxHistoryLimit {
		return monitoringapi.HistoryResponse{}, fmt.Errorf("%w: limit must be between 1 and %d", bridgeipc.ErrInvalidArgs, maxHistoryLimit)
	}
	if req.WindowMs < 0 {
		return monitoringapi.HistoryResponse{}, fmt.Errorf("%w: window_ms must be greater than zero", bridgeipc.ErrInvalidArgs)
	}
	if req.WindowMs > 0 && req.FromMs > 0 {
		return monitoringapi.HistoryResponse{}, fmt.Errorf("%w: from_ms and window_ms are mutually exclusive", bridgeipc.ErrInvalidArgs)
	}
	limit := req.Limit
	if limit == 0 {
		limit = defaultHistoryLimit
	}

	ctx, cancel := context.WithTimeout(ctx, historyReadTimeout)
	defer cancel()

	query := url.Values{}
	query.Set(monitoringapi.QueryResolution, string(req.Resolution))
	query.Set(monitoringapi.QueryLimit, strconv.Itoa(limit))
	fromMs := req.FromMs
	if req.WindowMs > 0 {
		fromMs = time.Now().UnixMilli() - req.WindowMs
	}
	if fromMs > 0 {
		query.Set(monitoringapi.QueryFrom, strconv.FormatInt(fromMs, 10))
	}
	if req.ToMs > 0 {
		query.Set(monitoringapi.QueryTo, strconv.FormatInt(req.ToMs, 10))
	}

	var envelope monitoringapi.HistoryResponse
	err := getJSON(ctx, controlClient, monitoringapi.RouteHistory(plugin)+"?"+query.Encode(), maxHistoryPayloadBytes, &envelope)
	if status, ok := errors.AsType[*statusError](err); ok {
		return monitoringapi.HistoryResponse{}, fmt.Errorf("history request failed (%s): %s", status.Status, agentErrorMessage(status.Body))
	}
	if err != nil {
		return monitoringapi.HistoryResponse{}, fmt.Errorf("monitoring history request: %w", err)
	}
	return envelope, nil
}

func agentErrorMessage(body []byte) string {
	var payload monitoringapi.ErrorResponse
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != "" {
		return payload.Error
	}
	return strings.TrimSpace(string(body))
}
