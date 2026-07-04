package monitoring

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func statusResponseWithListeners(listeners string) string {
	return `{
		"ok": true,
		"command": "status.get",
		"data": {
			"version": "1.2.3",
			"data_dir": "/var/lib/go-monitoring",
			"db_path": "/var/lib/go-monitoring/metrics.db",
			"collector_interval": "15s",
			"listeners": [` + listeners + `],
			"config": {"path": "", "source": "loaded", "version": 1, "collector_interval": "15s", "history_plugins": ["cpu"], "cache_ttl": {}},
			"retention": {"1m": "1h0m0s"}
		}
	}`
}

func withTestMetricsClient(t *testing.T, fn func(network, address string, req *http.Request) (*http.Response, error)) {
	t.Helper()
	orig := newMetricsClient
	newMetricsClient = func(network, address string) *http.Client {
		return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return fn(network, address, req)
		})}
	}
	t.Cleanup(func() { newMetricsClient = orig })
}

func TestFetchCPUHistoryFlattensPoints(t *testing.T) {
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		cmd := decodeCommandRequest(t, req)
		if cmd.Command != "status.get" {
			t.Fatalf("command = %q, want status.get", cmd.Command)
		}
		return jsonResponse(http.StatusOK, statusResponseWithListeners(
			`{"name": "metrics", "address": "192.168.1.66:45876", "effective_address": "192.168.1.66:45876", "apis": ["metrics"], "active": true}`,
		)), nil
	})
	withTestMetricsClient(t, func(network, address string, req *http.Request) (*http.Response, error) {
		if network != "tcp" || address != "192.168.1.66:45876" {
			t.Fatalf("dial = %s %s, want tcp 192.168.1.66:45876", network, address)
		}
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
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		decodeCommandRequest(t, req)
		return jsonResponse(http.StatusOK, statusResponseWithListeners(
			`{"name": "metrics", "address": "127.0.0.1:9000", "effective_address": "127.0.0.1:9000", "apis": ["metrics"], "active": true}`,
		)), nil
	})
	withTestMetricsClient(t, func(_, _ string, req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/v1/network/history" {
			t.Fatalf("path = %s, want /api/v1/network/history", req.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{
			"resolution": "10m",
			"items": [{"captured_at": 1700000060000, "stats": {"bandwidth_bytes_per_second": [43926, 1080230]}}]
		}`), nil
	})

	points, err := FetchNetworkHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "10m"})
	if err != nil {
		t.Fatalf("FetchNetworkHistory: %v", err)
	}
	if len(points) != 1 || points[0].SentBytesPerSec != 43926 || points[0].RecvBytesPerSec != 1080230 {
		t.Fatalf("points = %#v", points)
	}
}

func TestFetchDiskIOHistorySplitsReadWrite(t *testing.T) {
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		decodeCommandRequest(t, req)
		return jsonResponse(http.StatusOK, statusResponseWithListeners(
			`{"name": "metrics", "address": "127.0.0.1:9000", "effective_address": "127.0.0.1:9000", "apis": ["metrics"], "active": true}`,
		)), nil
	})
	withTestMetricsClient(t, func(_, _ string, req *http.Request) (*http.Response, error) {
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
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		decodeCommandRequest(t, req)
		return jsonResponse(http.StatusOK, statusResponseWithListeners(
			`{"name": "metrics", "address": "127.0.0.1:9000", "effective_address": "127.0.0.1:9000", "apis": ["metrics"], "active": true}`,
		)), nil
	})
	withTestMetricsClient(t, func(_, _ string, req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/v1/mem/history" {
			t.Fatalf("path = %s, want /api/v1/mem/history", req.URL.Path)
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
	if len(points) != 1 || points[0].UsedPercent != 39.03 || points[0].TotalGB != 15.41 {
		t.Fatalf("points = %#v", points)
	}
}

func TestFetchHistoryPrefersUnixMetricsListener(t *testing.T) {
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		decodeCommandRequest(t, req)
		return jsonResponse(http.StatusOK, statusResponseWithListeners(
			`{"name": "metrics", "address": "0.0.0.0:45876", "effective_address": "[::]:45876", "apis": ["metrics"], "active": true},
			{"name": "control", "address": "unix:/run/go-monitoring/agent.sock", "effective_address": "/run/go-monitoring/agent.sock", "apis": ["commands", "metrics"], "active": true}`,
		)), nil
	})
	withTestMetricsClient(t, func(network, address string, _ *http.Request) (*http.Response, error) {
		if network != "unix" || address != "/run/go-monitoring/agent.sock" {
			t.Fatalf("dial = %s %s, want unix socket", network, address)
		}
		return jsonResponse(http.StatusOK, `{"resolution": "1m", "items": []}`), nil
	})

	if _, err := FetchCPUHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "1m"}); err != nil {
		t.Fatalf("FetchCPUHistory: %v", err)
	}
}

func TestFetchHistoryNormalizesWildcardTCPHost(t *testing.T) {
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		decodeCommandRequest(t, req)
		return jsonResponse(http.StatusOK, statusResponseWithListeners(
			`{"name": "metrics", "address": "0.0.0.0:45876", "effective_address": "[::]:45876", "apis": ["metrics"], "active": true}`,
		)), nil
	})
	withTestMetricsClient(t, func(network, address string, _ *http.Request) (*http.Response, error) {
		if network != "tcp" || address != "127.0.0.1:45876" {
			t.Fatalf("dial = %s %s, want tcp 127.0.0.1:45876", network, address)
		}
		return jsonResponse(http.StatusOK, `{"resolution": "1m", "items": []}`), nil
	})

	if _, err := FetchCPUHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "1m"}); err != nil {
		t.Fatalf("FetchCPUHistory: %v", err)
	}
}

func TestFetchHistoryRejectsInvalidResolution(t *testing.T) {
	_, err := FetchCPUHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "5m"})
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

func TestFetchHistoryErrorsWithoutMetricsListener(t *testing.T) {
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		decodeCommandRequest(t, req)
		return jsonResponse(http.StatusOK, statusResponseWithListeners(
			`{"name": "control", "address": "unix:/run/go-monitoring/agent.sock", "effective_address": "/run/go-monitoring/agent.sock", "apis": ["commands"], "active": true}`,
		)), nil
	})

	_, err := FetchCPUHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "1m"})
	if err == nil || !strings.Contains(err.Error(), "no active metrics listener") {
		t.Fatalf("err = %v, want no active metrics listener", err)
	}
}

func TestFetchHistorySurfacesAgentError(t *testing.T) {
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		decodeCommandRequest(t, req)
		return jsonResponse(http.StatusOK, statusResponseWithListeners(
			`{"name": "metrics", "address": "127.0.0.1:9000", "effective_address": "127.0.0.1:9000", "apis": ["metrics"], "active": true}`,
		)), nil
	})
	withTestMetricsClient(t, func(_, _ string, _ *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusNotFound, `{"error": "history not enabled for plugin"}`), nil
	})

	_, err := FetchCPUHistory(context.Background(), apischema.MonitoringHistoryRequest{Resolution: "1m"})
	if err == nil || !strings.Contains(err.Error(), "history not enabled for plugin") {
		t.Fatalf("err = %v, want agent message", err)
	}
}
