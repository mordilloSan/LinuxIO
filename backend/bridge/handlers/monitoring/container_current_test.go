package monitoring

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFetchContainerMetricsSnapshotMapsLiveSamples(t *testing.T) {
	withTestAPIClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/v1/live" {
			t.Fatalf("path = %s, want /api/v1/live", req.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{
			"captured_at_ms": 1700000000000,
			"containers": {"captured_at_ms": 1700000000000, "items": [
				{"id":"abc123","name":"web","cpu_percent":50,"memory_bytes":1572864,"rx_bytes_per_sec":2048,"tx_bytes_per_sec":1024,"block_read_bytes_per_sec":4096,"block_write_bytes_per_sec":8192}
			]}
		}`), nil
	})

	snapshot, err := FetchContainerMetricsSnapshot(context.Background())
	if err != nil {
		t.Fatalf("FetchContainerMetricsSnapshot: %v", err)
	}
	if snapshot.CapturedAtMs != 1700000000000 {
		t.Fatalf("CapturedAtMs = %d", snapshot.CapturedAtMs)
	}
	if snapshot.CollectorInterval != 15*time.Second {
		t.Fatalf("CollectorInterval = %s, want 15s", snapshot.CollectorInterval)
	}
	sample, ok := snapshot.Samples["abc123"]
	if !ok {
		t.Fatalf("Samples = %#v, want abc123", snapshot.Samples)
	}
	// Live CPU already uses Docker's multi-core convention, so it is not scaled.
	if sample.CPUPercent != 50 {
		t.Fatalf("CPUPercent = %v, want 50", sample.CPUPercent)
	}
	if sample.MemoryUsageBytes != 1572864 {
		t.Fatalf("MemoryUsageBytes = %d, want 1572864", sample.MemoryUsageBytes)
	}
	if sample.NetworkSendBytesPerSecond != 1024 || sample.NetworkReceiveBytesPerSecond != 2048 {
		t.Fatalf("network rates = %v/%v", sample.NetworkSendBytesPerSecond, sample.NetworkReceiveBytesPerSecond)
	}
	if sample.BlockReadBytesPerSecond == nil || *sample.BlockReadBytesPerSecond != 4096 {
		t.Fatalf("BlockReadBytesPerSecond = %v", sample.BlockReadBytesPerSecond)
	}
	if sample.BlockWriteBytesPerSecond == nil || *sample.BlockWriteBytesPerSecond != 8192 {
		t.Fatalf("BlockWriteBytesPerSecond = %v", sample.BlockWriteBytesPerSecond)
	}
}

func TestFetchContainerMetricsSnapshotLeavesMissingTelemetryUnavailable(t *testing.T) {
	withTestAPIClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{
			"captured_at_ms": 1700000000000,
			"containers": {"captured_at_ms": 1700000000000, "items": [
				{"id":"abc123","name":"web","cpu_percent":1,"memory_bytes":1024,"rx_bytes_per_sec":0,"tx_bytes_per_sec":0}
			]}
		}`), nil
	})

	snapshot, err := FetchContainerMetricsSnapshot(context.Background())
	if err != nil {
		t.Fatalf("FetchContainerMetricsSnapshot: %v", err)
	}
	sample := snapshot.Samples["abc123"]
	if sample.BlockReadBytesPerSecond != nil || sample.BlockWriteBytesPerSecond != nil {
		t.Fatalf("block rates = %v/%v, want unavailable", sample.BlockReadBytesPerSecond, sample.BlockWriteBytesPerSecond)
	}
}

func TestFetchContainerMetricsSnapshotRejectsDuplicateIDs(t *testing.T) {
	withTestAPIClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{
			"containers": {"captured_at_ms": 1700000000000, "items": [
				{"id":"abc123","cpu_percent":1},
				{"id":"abc123","cpu_percent":2}
			]}
		}`), nil
	})

	_, err := FetchContainerMetricsSnapshot(context.Background())
	if err == nil || !strings.Contains(err.Error(), "duplicate container ID") {
		t.Fatalf("err = %v, want duplicate container ID", err)
	}
}

func TestFetchContainerMetricsSnapshotRejectsLiveFailure(t *testing.T) {
	withTestAPIClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusServiceUnavailable, `{"error":"collector unavailable"}`), nil
	})

	_, err := FetchContainerMetricsSnapshot(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestFetchContainerMetricsSnapshotHonorsCancellation(t *testing.T) {
	withTestAPIClient(t, func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := FetchContainerMetricsSnapshot(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
