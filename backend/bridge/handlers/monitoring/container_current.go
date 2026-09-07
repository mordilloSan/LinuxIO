package monitoring

import (
	"context"
	"fmt"
	"time"
)

// defaultContainerCollectorInterval is the staleness budget the Docker
// inventory derives its freshness window from. Live samples are collected per
// request, so this only bounds how long a sample may describe a container.
const defaultContainerCollectorInterval = 15 * time.Second

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

// ContainerMetricsSnapshot is the daemon's current container metrics view.
type ContainerMetricsSnapshot struct {
	CapturedAtMs      int64
	CollectorInterval time.Duration
	Samples           map[string]ContainerMetricSample
}

// FetchContainerMetricsSnapshot reads the container section of one live
// payload. Block rates stay nil when the daemon's telemetry collector has
// nothing to attribute to a container.
func FetchContainerMetricsSnapshot(ctx context.Context) (ContainerMetricsSnapshot, error) {
	live, err := FetchLive(ctx)
	if err != nil {
		return ContainerMetricsSnapshot{}, err
	}
	samples := make(map[string]ContainerMetricSample, len(live.Containers.Items))
	for index, item := range live.Containers.Items {
		if item.ID == "" {
			return ContainerMetricsSnapshot{}, fmt.Errorf("validate container metrics item %d: empty id", index)
		}
		if _, exists := samples[item.ID]; exists {
			return ContainerMetricsSnapshot{}, fmt.Errorf("validate container metrics item %d: duplicate container ID %q", index, item.ID)
		}
		// JSON cannot carry NaN or Inf, and the daemon derives rates from
		// monotonic counters, so a negative value is the only invalid case.
		if item.CPUPercent < 0 {
			return ContainerMetricsSnapshot{}, fmt.Errorf("validate container metrics item %d: invalid cpu percent", index)
		}
		samples[item.ID] = ContainerMetricSample{
			ID:                           item.ID,
			CPUPercent:                   item.CPUPercent,
			MemoryUsageBytes:             item.MemoryBytes,
			NetworkSendBytesPerSecond:    item.TxBytesPerSec,
			NetworkReceiveBytesPerSecond: item.RxBytesPerSec,
			BlockReadBytesPerSecond:      item.BlockReadBytesPerSec,
			BlockWriteBytesPerSecond:     item.BlockWriteBytesPerSec,
		}
	}
	return ContainerMetricsSnapshot{
		CapturedAtMs:      live.Containers.CapturedAtMs,
		CollectorInterval: defaultContainerCollectorInterval,
		Samples:           samples,
	}, nil
}
