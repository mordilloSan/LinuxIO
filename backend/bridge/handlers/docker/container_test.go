package docker

import (
	"errors"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/monitoring"
)

func TestContainerMetricsFromSnapshot(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	fullID := "abc123456789def123456789def123456789def123456789def123456789def1"
	readRate := float64(300)
	writeRate := float64(400)
	snapshot := monitoring.ContainerMetricsSnapshot{
		CapturedAtMs:      now.Add(-30 * time.Second).UnixMilli(),
		CollectorInterval: 15 * time.Second,
		Samples: map[string]monitoring.ContainerMetricSample{
			fullID[:12]: {
				ID:                           fullID[:12],
				CPUPercent:                   125.5,
				MemoryUsageBytes:             512 << 20,
				NetworkReceiveBytesPerSecond: 100,
				NetworkSendBytesPerSecond:    200,
				BlockReadBytesPerSecond:      &readRate,
				BlockWriteBytesPerSecond:     &writeRate,
			},
		},
	}
	ctr := container.Summary{ID: fullID, State: container.StateRunning}

	got := containerMetricsFromSnapshot(ctr, snapshot, nil, now)
	if got.Status != apischema.ContainerMetricsStatusAvailable || got.CapturedAtMs == nil || *got.CapturedAtMs != snapshot.CapturedAtMs {
		t.Fatalf("metrics status/capture = %#v", got)
	}
	if got.CPUPercent == nil || *got.CPUPercent != 125.5 || got.MemoryUsageBytes == nil || *got.MemoryUsageBytes != 512<<20 {
		t.Fatalf("CPU/memory metrics = %#v", got)
	}
	if got.NetworkReceiveBytesPerSecond == nil || *got.NetworkReceiveBytesPerSecond != 100 ||
		got.NetworkSendBytesPerSecond == nil || *got.NetworkSendBytesPerSecond != 200 {
		t.Fatalf("network rates = %#v", got)
	}
	if got.BlockReadBytesPerSecond == nil || *got.BlockReadBytesPerSecond != readRate ||
		got.BlockWriteBytesPerSecond == nil || *got.BlockWriteBytesPerSecond != writeRate {
		t.Fatalf("block rates = %#v", got)
	}
}

func TestContainerMetricsFromSnapshotReportsState(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	fullID := "abc123456789def123456789def123456789def123456789def123456789def1"
	sample := monitoring.ContainerMetricSample{ID: fullID[:12]}
	fresh := monitoring.ContainerMetricsSnapshot{
		CapturedAtMs:      now.UnixMilli(),
		CollectorInterval: 15 * time.Second,
		Samples:           map[string]monitoring.ContainerMetricSample{sample.ID: sample},
	}

	tests := []struct {
		name     string
		ctr      container.Summary
		snapshot monitoring.ContainerMetricsSnapshot
		err      error
		want     apischema.ContainerMetricsStatus
	}{
		{name: "stale", ctr: container.Summary{ID: fullID, State: container.StateRunning}, snapshot: monitoring.ContainerMetricsSnapshot{CapturedAtMs: now.Add(-2 * time.Minute).UnixMilli(), CollectorInterval: 15 * time.Second, Samples: fresh.Samples}, want: apischema.ContainerMetricsStatusStale},
		{name: "stopped", ctr: container.Summary{ID: fullID, State: container.StateExited}, snapshot: fresh, want: apischema.ContainerMetricsStatusNotRunning},
		{name: "sample missing", ctr: container.Summary{ID: "missing", State: container.StateRunning}, snapshot: fresh, want: apischema.ContainerMetricsStatusUnavailable},
		{name: "monitoring unavailable", ctr: container.Summary{ID: fullID, State: container.StateRunning}, snapshot: fresh, err: errors.New("offline"), want: apischema.ContainerMetricsStatusUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containerMetricsFromSnapshot(tt.ctr, tt.snapshot, tt.err, now)
			if got.Status != tt.want {
				t.Fatalf("status = %q, want %q", got.Status, tt.want)
			}
			if tt.want == apischema.ContainerMetricsStatusUnavailable || tt.want == apischema.ContainerMetricsStatusNotRunning {
				if got.CapturedAtMs != nil || got.CPUPercent != nil || got.MemoryUsageBytes != nil {
					t.Fatalf("unavailable metrics expose values: %#v", got)
				}
			}
		})
	}
}
