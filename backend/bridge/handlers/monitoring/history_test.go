package monitoring

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func withTestLogicalCPUCount(t *testing.T, fn func(context.Context, bool) (int, error)) {
	t.Helper()
	orig := logicalCPUCount
	logicalCPUCount = fn
	t.Cleanup(func() { logicalCPUCount = orig })
}

func withTestSingleLogicalCPU(t *testing.T) {
	t.Helper()
	withTestLogicalCPUCount(t, func(context.Context, bool) (int, error) { return 1, nil })
}

func TestFetchCPUHistoryFlattensPoints(t *testing.T) {
	withTestControlClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/v1/cpu/history" {
			t.Fatalf("path = %s, want /api/v1/cpu/history", req.URL.Path)
		}
		query := req.URL.Query()
		if query.Get("resolution") != "1m" || query.Get("limit") != "240" || query.Get("from") != "1700000000000" {
			t.Fatalf("query = %s", req.URL.RawQuery)
		}
		return jsonResponse(http.StatusOK, `{
			"resolution": "1m",
			"items": [
				{"captured_at": 1700000060000, "stats": {"cpu_percent": 8.3, "cpu_breakdown_percent": [3.9, 3.0, 1.4, 0, 91.7], "cpu_cores_percent": [12, 5]}},
				{"captured_at": 1700000120000, "stats": {"cpu_percent": 9.1}}
			]
		}`), nil
	})

	points, err := FetchCPUHistory(context.Background(), apischema.MonitoringHistoryRequest{
		Resolution: "1m",
		FromMs:     1700000000000,
		Limit:      240,
	})
	if err != nil {
		t.Fatalf("FetchCPUHistory: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("points = %#v, want 2", points)
	}
	if points[0].CapturedAtMs != 1700000060000 || points[0].UsagePercent != 8.3 {
		t.Fatalf("point[0] = %#v", points[0])
	}
	if len(points[0].BreakdownPercent) != 5 || len(points[0].CoresPercent) != 2 {
		t.Fatalf("point[0] arrays = %#v", points[0])
	}
	if points[1].UsagePercent != 9.1 || points[1].BreakdownPercent != nil {
		t.Fatalf("point[1] = %#v", points[1])
	}
}

func TestFetchNetworkHistorySplitsBandwidth(t *testing.T) {
	withTestControlClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/v1/network/history" {
			t.Fatalf("path = %s, want /api/v1/network/history", req.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{
			"resolution": "10m",
			"items": [{"captured_at": 1700000060000, "stats": {
				"bandwidth_bytes_per_second": [43926, 1080230],
				"network_interfaces": {"enp2s0": [43442, 1079765, 37447544910, 447722329331], "wg0": [484, 465, 1262132312, 190750568]}
			}}]
		}`), nil
	})

	points, err := FetchNetworkHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "10m"})
	if err != nil {
		t.Fatalf("FetchNetworkHistory: %v", err)
	}
	if len(points) != 1 || points[0].SentBytesPerSec != 43926 || points[0].RecvBytesPerSec != 1080230 {
		t.Fatalf("points = %#v", points)
	}
	if len(points[0].Interfaces) != 2 {
		t.Fatalf("interfaces = %#v", points[0].Interfaces)
	}
	if rates := points[0].Interfaces["enp2s0"]; rates.SentBytesPerSec != 43442 || rates.RecvBytesPerSec != 1079765 {
		t.Fatalf("enp2s0 rates = %#v", rates)
	}
}

func TestFetchDiskIOHistorySplitsReadWrite(t *testing.T) {
	withTestControlClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/v1/diskio/history" {
			t.Fatalf("path = %s, want /api/v1/diskio/history", req.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{
			"resolution": "1m",
			"items": [{"captured_at": 1700000060000, "stats": {"disk_io_bytes_per_second": [364, 360456]}}]
		}`), nil
	})

	points, err := FetchDiskIOHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "1m"})
	if err != nil {
		t.Fatalf("FetchDiskIOHistory: %v", err)
	}
	if len(points) != 1 || points[0].ReadBytesPerSec != 364 || points[0].WriteBytesPerSec != 360456 {
		t.Fatalf("points = %#v", points)
	}
}

func TestFetchMemoryHistoryDecodesStats(t *testing.T) {
	withTestControlClient(t, func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v1/mem/history":
			return jsonResponse(http.StatusOK, `{
				"resolution": "1m",
				"items": [{"captured_at": 1700000060000, "stats": {"memory_gb": 15.41, "memory_used_gb": 6.01, "memory_percent": 39.03, "memory_buffer_cache_gb": 8.44, "memory_cached_gb": 7.9, "memory_buffers_gb": 0.54}}]
			}`), nil
		case "/api/v1/containers/history":
			return jsonResponse(http.StatusOK, `{
				"resolution": "1m",
				"items": [{"captured_at": 1700000060000, "stats": [{"name": "a", "memory_mb": 512}, {"name": "b", "memory_mb": 1024}]}]
			}`), nil
		default:
			t.Fatalf("path = %s, want mem or containers history", req.URL.Path)
			return nil, nil
		}
	})

	points, err := FetchMemoryHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "1m"})
	if err != nil {
		t.Fatalf("FetchMemoryHistory: %v", err)
	}
	if len(points) != 1 || points[0].UsedPercent != 39.03 || points[0].TotalGB != 15.41 {
		t.Fatalf("points = %#v", points)
	}
	if points[0].CachedGB != 7.9 || points[0].BuffersGB != 0.54 {
		t.Fatalf("split cache fields = %#v", points[0])
	}
	if points[0].DockerUsedGB != 1.5 {
		t.Fatalf("DockerUsedGB = %v, want 1.5", points[0].DockerUsedGB)
	}
}

func TestFetchMemoryHistoryToleratesMissingContainersHistory(t *testing.T) {
	withTestControlClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/v1/containers/history" {
			return jsonResponse(http.StatusNotFound, `{"error": "history not enabled for plugin"}`), nil
		}
		return jsonResponse(http.StatusOK, `{
			"resolution": "1m",
			"items": [{"captured_at": 1700000060000, "stats": {"memory_gb": 15.41, "memory_used_gb": 6.01, "memory_percent": 39.03, "memory_buffer_cache_gb": 8.44}}]
		}`), nil
	})

	points, err := FetchMemoryHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "1m"})
	if err != nil {
		t.Fatalf("FetchMemoryHistory: %v", err)
	}
	if len(points) != 1 || points[0].DockerUsedGB != 0 {
		t.Fatalf("points = %#v", points)
	}
}

func TestFetchContainerHistoryMergesBlockIO(t *testing.T) {
	withTestSingleLogicalCPU(t)
	withTestControlClient(t, func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v1/containers/history":
			return jsonResponse(http.StatusOK, `{
				"resolution": "1m",
				"items": [{"captured_at": 1700000060000, "stats": [
					{"id": "abc123", "name": "web", "cpu_percent": 4.5, "memory_mb": 512, "bandwidth_bytes": [1024, 2048]},
					{"id": "def456", "name": "db", "cpu_percent": 1.25, "memory_mb": 1024}
				]}]
			}`), nil
		case "/api/v1/container_telemetry/history":
			// A few seconds off the containers bucket, and only one of the two
			// containers has processes the agent could attribute.
			return jsonResponse(http.StatusOK, `{
				"resolution": "1m",
				"items": [{"captured_at": 1700000058000, "stats": [
					{"id": "abc123", "name": "web", "disk_read_bytes_per_second": 4096, "disk_write_bytes_per_second": 8192}
				]}]
			}`), nil
		default:
			t.Fatalf("path = %s, want containers or container_telemetry history", req.URL.Path)
			return nil, nil
		}
	})

	points, err := FetchContainerHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "1m"})
	if err != nil {
		t.Fatalf("FetchContainerHistory: %v", err)
	}
	if len(points) != 1 || len(points[0].Containers) != 2 {
		t.Fatalf("points = %#v", points)
	}
	web := points[0].Containers[0]
	if web.ID != "abc123" || web.Name != "web" || web.CPUPercent != 4.5 || web.MemoryMB != 512 {
		t.Fatalf("web = %#v", web)
	}
	if web.SentBytesPerSec != 1024 || web.RecvBytesPerSec != 2048 {
		t.Fatalf("web bandwidth = %#v", web)
	}
	if web.ReadBytesPerSec == nil || *web.ReadBytesPerSec != 4096 {
		t.Fatalf("web read = %#v", web.ReadBytesPerSec)
	}
	if web.WriteBytesPerSec == nil || *web.WriteBytesPerSec != 8192 {
		t.Fatalf("web write = %#v", web.WriteBytesPerSec)
	}
	db := points[0].Containers[1]
	if db.ReadBytesPerSec != nil || db.WriteBytesPerSec != nil {
		t.Fatalf("db block I/O = %#v, want nil for a container telemetry never saw", db)
	}
}

func TestFetchContainerHistoryScalesCPUByLogicalCPUCount(t *testing.T) {
	withTestLogicalCPUCount(t, func(ctx context.Context, logical bool) (int, error) {
		if err := ctx.Err(); err != nil {
			t.Fatalf("CPU count context = %v", err)
		}
		if !logical {
			t.Fatal("CPU count query did not request logical CPUs")
		}
		return 4, nil
	})
	withTestControlClient(t, func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v1/containers/history":
			return jsonResponse(http.StatusOK, `{
				"resolution": "1m",
				"items": [{"captured_at": 1700000060000, "stats": [{"id": "abc123", "name": "web", "cpu_percent": 12.5}]}]
			}`), nil
		case "/api/v1/container_telemetry/history":
			return jsonResponse(http.StatusOK, `{"resolution":"1m","items":[]}`), nil
		default:
			t.Fatalf("path = %s, want containers or container_telemetry history", req.URL.Path)
			return nil, nil
		}
	})

	points, err := FetchContainerHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "1m"})
	if err != nil {
		t.Fatalf("FetchContainerHistory: %v", err)
	}
	if len(points) != 1 || len(points[0].Containers) != 1 {
		t.Fatalf("points = %#v", points)
	}
	if got := points[0].Containers[0].CPUPercent; got != 50 {
		t.Fatalf("CPUPercent = %v, want 50", got)
	}
}

func TestFetchContainerHistoryReturnsLogicalCPUCountError(t *testing.T) {
	wantErr := errors.New("cpu count unavailable")
	withTestLogicalCPUCount(t, func(ctx context.Context, logical bool) (int, error) {
		return 0, wantErr
	})

	_, err := FetchContainerHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "1m"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want wrapped CPU count error", err)
	}
}

func TestFetchContainerHistoryToleratesMissingTelemetry(t *testing.T) {
	withTestSingleLogicalCPU(t)
	withTestControlClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/v1/container_telemetry/history" {
			return jsonResponse(http.StatusNotFound, `{"error": "unknown plugin"}`), nil
		}
		return jsonResponse(http.StatusOK, `{
			"resolution": "1m",
			"items": [{"captured_at": 1700000060000, "stats": [{"id": "abc123", "name": "web", "cpu_percent": 4.5, "memory_mb": 512}]}]
		}`), nil
	})

	points, err := FetchContainerHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "1m"})
	if err != nil {
		t.Fatalf("FetchContainerHistory: %v", err)
	}
	if len(points) != 1 || len(points[0].Containers) != 1 {
		t.Fatalf("points = %#v", points)
	}
	if points[0].Containers[0].ReadBytesPerSec != nil {
		t.Fatalf("read = %#v, want nil when the plugin is unavailable", points[0].Containers[0].ReadBytesPerSec)
	}
}

func TestFetchContainerHistoryDropsBlockIOBeyondTolerance(t *testing.T) {
	withTestSingleLogicalCPU(t)
	withTestControlClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/v1/container_telemetry/history" {
			// Two resolution steps away: too far to describe this bucket.
			return jsonResponse(http.StatusOK, `{
				"resolution": "1m",
				"items": [{"captured_at": 1700000180000, "stats": [
					{"id": "abc123", "name": "web", "disk_read_bytes_per_second": 4096, "disk_write_bytes_per_second": 8192}
				]}]
			}`), nil
		}
		return jsonResponse(http.StatusOK, `{
			"resolution": "1m",
			"items": [{"captured_at": 1700000060000, "stats": [{"id": "abc123", "name": "web", "cpu_percent": 4.5, "memory_mb": 512}]}]
		}`), nil
	})

	points, err := FetchContainerHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "1m"})
	if err != nil {
		t.Fatalf("FetchContainerHistory: %v", err)
	}
	if points[0].Containers[0].ReadBytesPerSec != nil {
		t.Fatalf("read = %#v, want nil for a sample outside one resolution step", points[0].Containers[0].ReadBytesPerSec)
	}
}

func TestFetchHistoryRejectsInvalidResolution(t *testing.T) {
	_, err := FetchCPUHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "5m"})
	if !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Fatalf("err = %v, want ErrInvalidArgs", err)
	}
}

func TestFetchHistoryResolvesRollingWindowAtRequestTime(t *testing.T) {
	wantFrom := time.Now().Add(-time.Hour).UnixMilli()
	withTestControlClient(t, func(req *http.Request) (*http.Response, error) {
		from, err := strconv.ParseInt(req.URL.Query().Get("from"), 10, 64)
		if err != nil {
			t.Fatalf("from query: %v", err)
		}
		if delta := from - wantFrom; delta < 0 || delta > 5_000 {
			t.Fatalf("from = %d, want within 5s of %d", from, wantFrom)
		}
		return jsonResponse(http.StatusOK, `{"resolution":"1m","items":[]}`), nil
	})

	_, err := FetchCPUHistory(context.Background(), apischema.MonitoringHistoryRequest{
		Resolution: "1m",
		WindowMs:   int64(time.Hour / time.Millisecond),
	})
	if err != nil {
		t.Fatalf("FetchCPUHistory: %v", err)
	}
}

func TestFetchHistoryRejectsFromAndWindowTogether(t *testing.T) {
	_, err := FetchCPUHistory(context.Background(), apischema.MonitoringHistoryRequest{
		Resolution: "1m",
		FromMs:     1,
		WindowMs:   60_000,
	})
	if !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Fatalf("err = %v, want ErrInvalidArgs", err)
	}
}

func TestFetchHistoryRejectsOversizedLimit(t *testing.T) {
	_, err := FetchCPUHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "1m", Limit: maxHistoryLimit + 1})
	if !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Fatalf("err = %v, want ErrInvalidArgs", err)
	}
}

func TestFetchHistorySurfacesAgentError(t *testing.T) {
	withTestControlClient(t, func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusNotFound, `{"error": "history not enabled for plugin"}`), nil
	})

	_, err := FetchCPUHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "1m"})
	if err == nil || !strings.Contains(err.Error(), "history not enabled for plugin") {
		t.Fatalf("err = %v, want agent message", err)
	}
}

func withTestHistoryReadTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()
	orig := historyReadTimeout
	historyReadTimeout = timeout
	t.Cleanup(func() { historyReadTimeout = orig })
}

func TestFetchHistoryBoundsSlowReads(t *testing.T) {
	withTestHistoryReadTimeout(t, 50*time.Millisecond)
	withTestControlClient(t, func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})

	_, err := FetchCPUHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "1m"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}
