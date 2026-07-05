package monitoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

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

// newMetricsClient builds the HTTP client used to reach the agent's metrics
// listener. Overridable in tests.
var newMetricsClient = func(network, address string) *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				dialer := &net.Dialer{Timeout: 5 * time.Second}
				return dialer.DialContext(ctx, network, address)
			},
		},
	}
}

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
	MemMB float64 `json:"memory_mb"`
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

	// Both plugins snapshot on the same collector tick, but rollup buckets can
	// land a little apart, so match each memory point to the nearest container
	// sample within one resolution step. Both series are in ascending order.
	tolerance := resolutionStepMs(req.Resolution)
	j := 0
	for i := range points {
		t := points[i].CapturedAtMs
		for j+1 < len(docker) && absInt64(docker[j+1].capturedAtMs-t) <= absInt64(docker[j].capturedAtMs-t) {
			j++
		}
		if absInt64(docker[j].capturedAtMs-t) <= tolerance {
			points[i].DockerUsedGB = docker[j].usedGB
		}
	}
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
	limit := req.Limit
	if limit == 0 {
		limit = defaultHistoryLimit
	}

	network, address, err := resolveMetricsListener(ctx)
	if err != nil {
		return historyEnvelope{}, err
	}

	query := url.Values{}
	query.Set("resolution", string(req.Resolution))
	query.Set("limit", strconv.Itoa(limit))
	if req.FromMs > 0 {
		query.Set("from", strconv.FormatInt(req.FromMs, 10))
	}
	if req.ToMs > 0 {
		query.Set("to", strconv.FormatInt(req.ToMs, 10))
	}

	host := "unix"
	if network == "tcp" {
		host = address
	}
	requestURL := fmt.Sprintf("http://%s/api/v1/%s/history?%s", host, plugin, query.Encode())
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return historyEnvelope{}, fmt.Errorf("create history request: %w", err)
	}

	resp, err := newMetricsClient(network, address).Do(httpReq)
	if err != nil {
		return historyEnvelope{}, fmt.Errorf("monitoring metrics request: %w", err)
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

// resolveMetricsListener finds an active agent listener that serves the
// metrics API, preferring unix sockets over TCP. Wildcard TCP hosts are
// rewritten to loopback since the bridge runs on the same machine.
func resolveMetricsListener(ctx context.Context) (network, address string, err error) {
	status, err := FetchStatus(ctx)
	if err != nil {
		return "", "", err
	}

	var tcpAddress string
	for _, listener := range status.Listeners {
		if !listener.Active || !servesMetricsAPI(listener.APIs) {
			continue
		}
		addr := listener.EffectiveAddress
		if addr == "" {
			addr = listener.Address
		}
		if path, ok := unixSocketPath(addr); ok {
			return "unix", path, nil
		}
		if tcpAddress == "" {
			tcpAddress = normalizeTCPAddress(addr)
		}
	}
	if tcpAddress != "" {
		return "tcp", tcpAddress, nil
	}
	return "", "", errors.New("monitoring agent has no active metrics listener")
}

func servesMetricsAPI(apis []string) bool {
	for _, api := range apis {
		if strings.EqualFold(strings.TrimSpace(api), "metrics") {
			return true
		}
	}
	return false
}

func unixSocketPath(addr string) (string, bool) {
	if path, ok := strings.CutPrefix(addr, "unix:"); ok {
		return path, true
	}
	if strings.HasPrefix(addr, "/") {
		return addr, true
	}
	return "", false
}

func normalizeTCPAddress(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	switch host {
	case "", "::", "0.0.0.0":
		return net.JoinHostPort("127.0.0.1", port)
	}
	return addr
}
