package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

const (
	maxHistoryPayloadBytes = 8 << 20
	maxHistoryLimit        = 1000
	defaultHistoryLimit    = 100
)

var historyResolutions = map[apischema.MonitoringHistoryResolution]struct{}{
	"1m": {}, "10m": {}, "20m": {}, "120m": {}, "480m": {},
}

// historyReadTimeout bounds one history read. controlClient stays unbounded so
// long-running db.maintain commands survive, so the deadline lives here.
var historyReadTimeout = 15 * time.Second

// logicalCPUCount is injectable so history conversion tests can use a
// deterministic CPU count while production requests retain gopsutil's
// context-aware host query.
var logicalCPUCount = cpu.CountsWithContext

type historyEnvelope struct {
	Resolution string        `json:"resolution"`
	Items      []historyItem `json:"items"`
}

type historyItem struct {
	CapturedAt int64           `json:"captured_at"`
	Stats      json.RawMessage `json:"stats"`
}

// Agent-side stats payloads for the plugins surfaced as hardware charts.
type cpuHistoryStats struct {
	CPUPercent       float64   `json:"cpu_percent"`
	BreakdownPercent []float64 `json:"cpu_breakdown_percent"`
	CoresPercent     []float64 `json:"cpu_cores_percent"`
}

type memHistoryStats struct {
	TotalGB       float64 `json:"memory_gb"`
	UsedGB        float64 `json:"memory_used_gb"`
	UsedPercent   float64 `json:"memory_percent"`
	BufferCacheGB float64 `json:"memory_buffer_cache_gb"`
	ZFSArcGB      float64 `json:"memory_zfs_arc_gb"`
	// Split cache fields; absent (zero) on agents older than v1.5.
	CachedGB  float64 `json:"memory_cached_gb"`
	BuffersGB float64 `json:"memory_buffers_gb"`
}

// containerHistoryRecord is one container's entry inside a containers-plugin
// history point, whose stats payload is an array of these records.
type containerHistoryRecord struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	CPUPct float64 `json:"cpu_percent"`
	MemMB  float64 `json:"memory_mb"`
	// Bandwidth is [sent, received] bytes per second.
	Bandwidth [2]uint64 `json:"bandwidth_bytes"`
}

// containerTelemetryRecord is one container's entry inside a
// container_telemetry-plugin history point. That plugin attributes host
// processes to containers, so it carries the block I/O the containers plugin
// does not collect.
type containerTelemetryRecord struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ReadBytesPerSec  uint64 `json:"disk_read_bytes_per_second"`
	WriteBytesPerSec uint64 `json:"disk_write_bytes_per_second"`
}

type diskIOHistoryStats struct {
	// IOBytesPerSec is [read, write].
	IOBytesPerSec [2]float64 `json:"disk_io_bytes_per_second"`
}

type networkHistoryStats struct {
	// BandwidthBytesPerSec is [sent, received].
	BandwidthBytesPerSec [2]float64 `json:"bandwidth_bytes_per_second"`
	// NetworkInterfaces values are [sent/s, received/s, total sent, total received].
	NetworkInterfaces map[string][4]float64 `json:"network_interfaces"`
}

func FetchCPUHistory(ctx context.Context, req apischema.MonitoringHistoryRequest) ([]apischema.MonitoringCPUHistoryPoint, error) {
	return fetchHistory(ctx, "cpu", req, func(item historyItem, stats cpuHistoryStats) apischema.MonitoringCPUHistoryPoint {
		return apischema.MonitoringCPUHistoryPoint{
			CapturedAtMs:     item.CapturedAt,
			UsagePercent:     stats.CPUPercent,
			BreakdownPercent: stats.BreakdownPercent,
			CoresPercent:     stats.CoresPercent,
		}
	})
}

func FetchMemoryHistory(ctx context.Context, req apischema.MonitoringHistoryRequest) ([]apischema.MonitoringMemoryHistoryPoint, error) {
	points, err := fetchHistory(ctx, "mem", req, func(item historyItem, stats memHistoryStats) apischema.MonitoringMemoryHistoryPoint {
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
	docker, err := fetchHistory(ctx, "containers", req, func(item historyItem, records []containerHistoryRecord) dockerMemHistoryPoint {
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

	points, err := fetchHistory(ctx, "containers", req, func(item historyItem, records []containerHistoryRecord) apischema.MonitoringContainerHistoryPoint {
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
	byContainer  map[string]containerTelemetryRecord
}

// mergeContainerDiskHistory annotates each container sample with the block I/O
// captured nearest to it. Best-effort: container_telemetry is a separate
// history plugin an operator can disable, and an agent older than v1.7 does
// not have it at all, so failures leave the disk fields nil rather than zero.
func mergeContainerDiskHistory(ctx context.Context, req apischema.MonitoringHistoryRequest, points []apischema.MonitoringContainerHistoryPoint) {
	if len(points) == 0 {
		return
	}
	telemetry, err := fetchHistory(ctx, "container_telemetry", req, func(item historyItem, records []containerTelemetryRecord) containerDiskHistoryPoint {
		byContainer := make(map[string]containerTelemetryRecord, len(records))
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
			read := float64(record.ReadBytesPerSec)
			write := float64(record.WriteBytesPerSec)
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
	minutes, err := strconv.Atoi(strings.TrimSuffix(string(resolution), "m"))
	if err != nil || minutes <= 0 {
		return 60_000
	}
	return int64(minutes) * 60_000
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func FetchDiskIOHistory(ctx context.Context, req apischema.MonitoringHistoryRequest) ([]apischema.MonitoringDiskIOHistoryPoint, error) {
	return fetchHistory(ctx, "diskio", req, func(item historyItem, stats diskIOHistoryStats) apischema.MonitoringDiskIOHistoryPoint {
		return apischema.MonitoringDiskIOHistoryPoint{
			CapturedAtMs:     item.CapturedAt,
			ReadBytesPerSec:  stats.IOBytesPerSec[0],
			WriteBytesPerSec: stats.IOBytesPerSec[1],
		}
	})
}

func FetchNetworkHistory(ctx context.Context, req apischema.MonitoringHistoryRequest) ([]apischema.MonitoringNetworkHistoryPoint, error) {
	return fetchHistory(ctx, "network", req, func(item historyItem, stats networkHistoryStats) apischema.MonitoringNetworkHistoryPoint {
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

func fetchHistory[S, P any](ctx context.Context, plugin string, req apischema.MonitoringHistoryRequest, flatten func(historyItem, S) P) ([]P, error) {
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

func fetchHistoryEnvelope(ctx context.Context, plugin string, req apischema.MonitoringHistoryRequest) (historyEnvelope, error) {
	if _, ok := historyResolutions[req.Resolution]; !ok {
		return historyEnvelope{}, fmt.Errorf("%w: invalid resolution %q", bridgeipc.ErrInvalidArgs, req.Resolution)
	}
	if req.Limit < 0 || req.Limit > maxHistoryLimit {
		return historyEnvelope{}, fmt.Errorf("%w: limit must be between 1 and %d", bridgeipc.ErrInvalidArgs, maxHistoryLimit)
	}
	if req.WindowMs < 0 {
		return historyEnvelope{}, fmt.Errorf("%w: window_ms must be greater than zero", bridgeipc.ErrInvalidArgs)
	}
	if req.WindowMs > 0 && req.FromMs > 0 {
		return historyEnvelope{}, fmt.Errorf("%w: from_ms and window_ms are mutually exclusive", bridgeipc.ErrInvalidArgs)
	}
	limit := req.Limit
	if limit == 0 {
		limit = defaultHistoryLimit
	}

	ctx, cancel := context.WithTimeout(ctx, historyReadTimeout)
	defer cancel()

	query := url.Values{}
	query.Set("resolution", string(req.Resolution))
	query.Set("limit", strconv.Itoa(limit))
	fromMs := req.FromMs
	if req.WindowMs > 0 {
		fromMs = time.Now().UnixMilli() - req.WindowMs
	}
	if fromMs > 0 {
		query.Set("from", strconv.FormatInt(fromMs, 10))
	}
	if req.ToMs > 0 {
		query.Set("to", strconv.FormatInt(req.ToMs, 10))
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/api/v1/"+plugin+"/history?"+query.Encode(), nil)
	if err != nil {
		return historyEnvelope{}, fmt.Errorf("create history request: %w", err)
	}

	resp, err := controlClient.Do(httpReq)
	if err != nil {
		return historyEnvelope{}, fmt.Errorf("monitoring history request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHistoryPayloadBytes))
	if err != nil {
		return historyEnvelope{}, fmt.Errorf("read history response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return historyEnvelope{}, fmt.Errorf("history request failed (%s): %s", resp.Status, agentErrorMessage(body))
	}

	var envelope historyEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return historyEnvelope{}, fmt.Errorf("decode history response: %w", err)
	}
	return envelope, nil
}

func agentErrorMessage(body []byte) string {
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != "" {
		return payload.Error
	}
	return strings.TrimSpace(string(body))
}
