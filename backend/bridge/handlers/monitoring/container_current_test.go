package monitoring

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFetchContainerMetricsSnapshotMapsCurrentSamples(t *testing.T) {
	withTestLogicalCPUCount(t, func(context.Context, bool) (int, error) { return 4, nil })
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		decodeCommandRequest(t, req)
		return jsonResponse(http.StatusOK, statusResponseWithListeners(
			`{"name":"metrics","address":"127.0.0.1:9000","effective_address":"127.0.0.1:9000","apis":["metrics"],"active":true}`,
		)), nil
	})
	withTestMetricsClient(t, func(network, address string, req *http.Request) (*http.Response, error) {
		if network != "tcp" || address != "127.0.0.1:9000" {
			t.Fatalf("dial = %s %s, want tcp 127.0.0.1:9000", network, address)
		}
		switch req.URL.Path {
		case "/api/v1/containers":
			return jsonResponse(http.StatusOK, `{
				"captured_at": 1700000000000,
				"items": [{"id":"abc123","name":"web","cpu_percent":12.5,"memory_mb":1.5,"bandwidth_bytes":[1024,2048]}]
			}`), nil
		case "/api/v1/container_telemetry":
			return jsonResponse(http.StatusOK, `{
				"captured_at": 1700000000010,
				"items": [{"id":"abc123","name":"web","disk_read_bytes_per_second":4096,"disk_write_bytes_per_second":8192}]
			}`), nil
		default:
			t.Fatalf("path = %s", req.URL.Path)
			return nil, nil
		}
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

func TestFetchContainerMetricsSnapshotLeavesTelemetryOutsideIntervalUnavailable(t *testing.T) {
	withTestSingleLogicalCPU(t)
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		decodeCommandRequest(t, req)
		return jsonResponse(http.StatusOK, statusResponseWithListeners(
			`{"name":"metrics","address":"127.0.0.1:9000","effective_address":"127.0.0.1:9000","apis":["metrics"],"active":true}`,
		)), nil
	})
	withTestMetricsClient(t, func(_, _ string, req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/v1/container_telemetry" {
			return jsonResponse(http.StatusOK, `{"captured_at":1700000016000,"items":[{"id":"abc123","disk_read_bytes_per_second":4096,"disk_write_bytes_per_second":8192}]}`), nil
		}
		return jsonResponse(http.StatusOK, `{"captured_at":1700000000000,"items":[{"id":"abc123","cpu_percent":1,"memory_mb":1,"bandwidth_bytes":[0,0]}]}`), nil
	})

	snapshot, err := FetchContainerMetricsSnapshot(context.Background())
	if err != nil {
		t.Fatalf("FetchContainerMetricsSnapshot: %v", err)
	}
	if sample := snapshot.Samples["abc123"]; sample.BlockReadBytesPerSecond != nil || sample.BlockWriteBytesPerSecond != nil {
		t.Fatalf("block rates = %v/%v, want unavailable", sample.BlockReadBytesPerSecond, sample.BlockWriteBytesPerSecond)
	}
}

func TestFetchContainerMetricsSnapshotToleratesTelemetryFailure(t *testing.T) {
	withTestSingleLogicalCPU(t)
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		decodeCommandRequest(t, req)
		return jsonResponse(http.StatusOK, statusResponseWithListeners(
			`{"name":"metrics","address":"127.0.0.1:9000","effective_address":"127.0.0.1:9000","apis":["metrics"],"active":true}`,
		)), nil
	})
	withTestMetricsClient(t, func(_, _ string, req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/v1/container_telemetry" {
			return jsonResponse(http.StatusNotFound, `{"error":"unknown plugin"}`), nil
		}
		return jsonResponse(http.StatusOK, `{"captured_at":1700000000000,"items":[{"id":"abc123","cpu_percent":1,"memory_mb":1,"bandwidth_bytes":[0,0]}]}`), nil
	})

	snapshot, err := FetchContainerMetricsSnapshot(context.Background())
	if err != nil {
		t.Fatalf("FetchContainerMetricsSnapshot: %v", err)
	}
	if len(snapshot.Samples) != 1 {
		t.Fatalf("Samples = %#v", snapshot.Samples)
	}
	if sample := snapshot.Samples["abc123"]; sample.BlockReadBytesPerSecond != nil || sample.BlockWriteBytesPerSecond != nil {
		t.Fatalf("block rates = %v/%v, want unavailable", sample.BlockReadBytesPerSecond, sample.BlockWriteBytesPerSecond)
	}
}

func TestFetchContainerMetricsSnapshotRejectsCurrentFailure(t *testing.T) {
	withTestSingleLogicalCPU(t)
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		decodeCommandRequest(t, req)
		return jsonResponse(http.StatusOK, statusResponseWithListeners(
			`{"name":"metrics","address":"127.0.0.1:9000","effective_address":"127.0.0.1:9000","apis":["metrics"],"active":true}`,
		)), nil
	})
	withTestMetricsClient(t, func(_, _ string, _ *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusBadGateway, `{"error":"collector unavailable"}`), nil
	})

	_, err := FetchContainerMetricsSnapshot(context.Background())
	if err == nil || !strings.Contains(err.Error(), "collector unavailable") {
		t.Fatalf("err = %v, want current collector error", err)
	}
}

func TestFetchContainerMetricsSnapshotHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := FetchContainerMetricsSnapshot(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestContainerMetricSampleValidatesValues(t *testing.T) {
	valid := containerMetricRecord{ID: "abc123", CPUPercent: 125, MemoryMB: 1, Bandwidth: [2]float64{1, 2}}
	sample, err := containerMetricSample(valid, 2)
	if err != nil {
		t.Fatalf("containerMetricSample(valid): %v", err)
	}
	if sample.CPUPercent != 250 {
		t.Fatalf("CPUPercent = %v, want 250", sample.CPUPercent)
	}
	zeroRates, err := containerMetricSample(containerMetricRecord{ID: "zero", CPUPercent: 1, MemoryMB: 1}, 1)
	if err != nil {
		t.Fatalf("containerMetricSample(omitted bandwidth): %v", err)
	}
	if zeroRates.NetworkSendBytesPerSecond != 0 || zeroRates.NetworkReceiveBytesPerSecond != 0 {
		t.Fatalf("omitted bandwidth = %v/%v, want measured zero", zeroRates.NetworkSendBytesPerSecond, zeroRates.NetworkReceiveBytesPerSecond)
	}

	tests := []containerMetricRecord{
		{CPUPercent: 1, MemoryMB: 1, Bandwidth: [2]float64{1, 2}},
		{ID: "abc123", CPUPercent: -1, MemoryMB: 1, Bandwidth: [2]float64{1, 2}},
		{ID: "abc123", CPUPercent: math.NaN(), MemoryMB: 1, Bandwidth: [2]float64{1, 2}},
		{ID: "abc123", CPUPercent: 1, MemoryMB: -1, Bandwidth: [2]float64{1, 2}},
		{ID: "abc123", CPUPercent: 1, MemoryMB: 1, Bandwidth: [2]float64{-1, 2}},
	}
	for _, test := range tests {
		if _, err := containerMetricSample(test, 1); err == nil {
			t.Fatalf("containerMetricSample(%#v) succeeded, want validation error", test)
		}
	}
}

func TestParseContainerCollectorIntervalUsesSafeDefault(t *testing.T) {
	for _, value := range []string{"", "invalid", "0s", "-1s", "2h"} {
		if interval, ok := parseContainerCollectorInterval(value); ok || interval != 0 {
			t.Fatalf("parseContainerCollectorInterval(%q) = %s, %v, want invalid", value, interval, ok)
		}
	}
	if interval := defaultContainerCollectorInterval; interval != 15*time.Second {
		t.Fatalf("default interval = %s, want 15s", interval)
	}
}

func TestWithinCollectorInterval(t *testing.T) {
	if !withinCollectorInterval(1_000, 1_015, 15*time.Millisecond) {
		t.Fatal("equal interval should match")
	}
	if withinCollectorInterval(1_000, 1_016, 15*time.Millisecond) {
		t.Fatal("outside interval should not match")
	}
	if withinCollectorInterval(0, 1_000, time.Second) {
		t.Fatal("missing capture time should not match")
	}
}
