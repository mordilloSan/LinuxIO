package monitoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

const (
	defaultContainerCollectorInterval = 15 * time.Second
	maxContainerMetricItems           = 4096
	containerMemoryBytesPerMB         = 1024 * 1024
)

// ContainerMetricSample is one current container metrics sample. CPU uses the
// Docker multi-core percentage convention; network and block I/O are rates.
type ContainerMetricSample struct {
	ID                           string
	CPUPercent                   float64
	MemoryUsageBytes             uint64
	NetworkSendBytesPerSecond    float64
	NetworkReceiveBytesPerSecond float64
	BlockReadBytesPerSecond      *float64
	BlockWriteBytesPerSecond     *float64
}

// ContainerMetricsSnapshot is the agent's current container metrics view.
type ContainerMetricsSnapshot struct {
	CapturedAtMs      int64
	CollectorInterval time.Duration
	Samples           map[string]ContainerMetricSample
}

type containerMetricsResponse struct {
	CapturedAt int64                   `json:"captured_at"`
	Items      []containerMetricRecord `json:"items"`
}

type containerMetricRecord struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CPUPercent float64 `json:"cpu_percent"`
	MemoryMB   float64 `json:"memory_mb"`
	// v1.7 omits this zero-valued field on a current first sample; an array
	// keeps that omission equivalent to measured zero traffic.
	Bandwidth [2]float64 `json:"bandwidth_bytes"`
}

type containerTelemetryResponse struct {
	CapturedAt int64                      `json:"captured_at"`
	Items      []containerTelemetryRecord `json:"items"`
}

// FetchContainerMetricsSnapshot reads one bounded current metrics response
// from go-monitoring. The separate telemetry plugin is best effort so older
// agents and disabled plugins leave block rates unavailable.
//
//nolint:gocognit // One bounded read coordinates required current data with optional telemetry.
func FetchContainerMetricsSnapshot(ctx context.Context) (ContainerMetricsSnapshot, error) {
	var empty ContainerMetricsSnapshot
	if err := ctx.Err(); err != nil {
		return empty, err
	}

	status, err := FetchStatus(ctx)
	if err != nil {
		return empty, fmt.Errorf("fetch monitoring status for container metrics: %w", err)
	}
	interval := containerCollectorInterval(status)
	network, address, err := resolveMetricsListenerFromStatus(status)
	if err != nil {
		return empty, err
	}
	logicalCPUs, err := logicalCPUCount(ctx, true)
	if err != nil {
		return empty, fmt.Errorf("count logical CPUs for container metrics: %w", err)
	}
	if logicalCPUs <= 0 {
		return empty, fmt.Errorf("count logical CPUs for container metrics: got %d", logicalCPUs)
	}

	response, err := fetchContainerMetricsResponse(ctx, network, address)
	if err != nil {
		return empty, err
	}

	samples := make(map[string]ContainerMetricSample, len(response.Items))
	for index, item := range response.Items {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return empty, ctxErr
		}
		sample, sampleErr := containerMetricSample(item, logicalCPUs)
		if sampleErr != nil {
			return empty, fmt.Errorf("validate container metrics item %d: %w", index, sampleErr)
		}
		if _, exists := samples[sample.ID]; exists {
			return empty, fmt.Errorf("validate container metrics item %d: duplicate container ID %q", index, sample.ID)
		}
		samples[sample.ID] = sample
	}

	telemetry, telemetryCapturedAt, err := fetchContainerTelemetry(ctx, network, address)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return empty, err
		}
	} else if withinCollectorInterval(response.CapturedAt, telemetryCapturedAt, interval) {
		for id, rates := range telemetry {
			sample, ok := samples[id]
			if !ok {
				continue
			}
			read, write := rates[0], rates[1]
			sample.BlockReadBytesPerSecond = &read
			sample.BlockWriteBytesPerSecond = &write
			samples[id] = sample
		}
	}

	return ContainerMetricsSnapshot{
		CapturedAtMs:      response.CapturedAt,
		CollectorInterval: interval,
		Samples:           samples,
	}, nil
}

func containerCollectorInterval(status apischema.MonitoringStatus) time.Duration {
	for _, value := range []string{status.CollectorInterval, status.Config.CollectorInterval} {
		if interval, ok := parseContainerCollectorInterval(value); ok {
			return interval
		}
	}
	return defaultContainerCollectorInterval
}

func parseContainerCollectorInterval(value string) (time.Duration, bool) {
	interval, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || interval <= 0 || interval > time.Hour {
		return 0, false
	}
	return interval, true
}

func fetchContainerMetricsResponse(ctx context.Context, network, address string) (containerMetricsResponse, error) {
	body, err := fetchContainerMetricsEndpoint(ctx, network, address, "/api/v1/containers")
	if err != nil {
		return containerMetricsResponse{}, fmt.Errorf("fetch current container metrics: %w", err)
	}

	var response containerMetricsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return containerMetricsResponse{}, fmt.Errorf("decode current container metrics: %w", err)
	}
	if response.CapturedAt <= 0 {
		return containerMetricsResponse{}, errors.New("decode current container metrics: missing captured_at")
	}
	if len(response.Items) > maxContainerMetricItems {
		return containerMetricsResponse{}, fmt.Errorf("decode current container metrics: item count exceeds %d", maxContainerMetricItems)
	}
	return response, nil
}

func fetchContainerTelemetry(ctx context.Context, network, address string) (map[string][2]float64, int64, error) {
	body, err := fetchContainerMetricsEndpoint(ctx, network, address, "/api/v1/container_telemetry")
	if err != nil {
		return nil, 0, err
	}

	var response containerTelemetryResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, 0, fmt.Errorf("decode current container telemetry: %w", err)
	}
	if response.CapturedAt <= 0 {
		return nil, 0, errors.New("decode current container telemetry: missing captured_at")
	}
	if len(response.Items) > maxContainerMetricItems {
		return nil, 0, fmt.Errorf("decode current container telemetry: item count exceeds %d", maxContainerMetricItems)
	}

	rates := make(map[string][2]float64, len(response.Items))
	for index, item := range response.Items {
		id := strings.TrimSpace(item.ID)
		if err := validateContainerID(id); err != nil {
			return nil, 0, fmt.Errorf("validate current container telemetry item %d: %w", index, err)
		}
		if err := validateMetricValue(float64(item.ReadBytesPerSec), "read bytes per second"); err != nil {
			return nil, 0, fmt.Errorf("validate current container telemetry item %d: %w", index, err)
		}
		if err := validateMetricValue(float64(item.WriteBytesPerSec), "write bytes per second"); err != nil {
			return nil, 0, fmt.Errorf("validate current container telemetry item %d: %w", index, err)
		}
		if _, exists := rates[id]; exists {
			return nil, 0, fmt.Errorf("validate current container telemetry item %d: duplicate container ID %q", index, id)
		}
		rates[id] = [2]float64{float64(item.ReadBytesPerSec), float64(item.WriteBytesPerSec)}
	}
	return rates, response.CapturedAt, nil
}

func fetchContainerMetricsEndpoint(ctx context.Context, network, address, path string) ([]byte, error) {
	host := "unix"
	if network == "tcp" {
		host = address
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+host+path, nil)
	if err != nil {
		return nil, fmt.Errorf("create current container metrics request: %w", err)
	}

	response, err := newMetricsClient(network, address).Do(request)
	if err != nil {
		return nil, fmt.Errorf("monitoring metrics request: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxHistoryPayloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read current container metrics response: %w", err)
	}
	if len(body) > maxHistoryPayloadBytes {
		return nil, fmt.Errorf("current container metrics response exceeds %d bytes", maxHistoryPayloadBytes)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("current container metrics request failed (%s): %s", response.Status, agentErrorMessage(body))
	}
	return body, nil
}

func containerMetricSample(item containerMetricRecord, logicalCPUs int) (ContainerMetricSample, error) {
	if logicalCPUs <= 0 {
		return ContainerMetricSample{}, fmt.Errorf("logical CPU count must be positive, got %d", logicalCPUs)
	}
	id := strings.TrimSpace(item.ID)
	if err := validateContainerID(id); err != nil {
		return ContainerMetricSample{}, err
	}
	if err := validateMetricValue(item.CPUPercent, "CPU percent"); err != nil {
		return ContainerMetricSample{}, err
	}
	cpuPercent := item.CPUPercent * float64(logicalCPUs)
	if err := validateMetricValue(cpuPercent, "Docker CPU percent"); err != nil {
		return ContainerMetricSample{}, err
	}
	if err := validateMetricValue(item.MemoryMB, "memory MB"); err != nil {
		return ContainerMetricSample{}, err
	}
	if item.MemoryMB >= float64(^uint64(0))/containerMemoryBytesPerMB {
		return ContainerMetricSample{}, errors.New("memory MB exceeds uint64 byte range")
	}
	if err := validateMetricValue(item.Bandwidth[0], "network send bytes per second"); err != nil {
		return ContainerMetricSample{}, err
	}
	if err := validateMetricValue(item.Bandwidth[1], "network receive bytes per second"); err != nil {
		return ContainerMetricSample{}, err
	}

	return ContainerMetricSample{
		ID:                           id,
		CPUPercent:                   cpuPercent,
		MemoryUsageBytes:             uint64(item.MemoryMB * containerMemoryBytesPerMB),
		NetworkSendBytesPerSecond:    item.Bandwidth[0],
		NetworkReceiveBytesPerSecond: item.Bandwidth[1],
	}, nil
}

func validateContainerID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("container ID is required")
	}
	return nil
}

func validateMetricValue(value float64, name string) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return fmt.Errorf("%s must be finite and non-negative", name)
	}
	return nil
}

func withinCollectorInterval(left, right int64, interval time.Duration) bool {
	if left <= 0 || right <= 0 {
		return false
	}
	if left >= right {
		return left-right <= interval.Milliseconds()
	}
	return right-left <= interval.Milliseconds()
}
