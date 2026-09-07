package monitoring

import (
	"context"
	"errors"
	"net/http"
	"syscall"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestFetchProcessesDecodesDaemonEnvelope(t *testing.T) {
	withTestAPIClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != processesRoute {
			t.Fatalf("path = %s, want %s", req.URL.Path, processesRoute)
		}
		return jsonResponse(http.StatusOK, `{
			"captured_at": 1700000000000,
			"count": {"total": 2, "running": 1, "sleeping": 1, "thread": 3},
			"items": [{"pid": 42, "name": "worker", "cpu_percent": 12.5, "memory_percent": 2.5, "memory_info": {"rss": 4096, "vms": 8192}, "io_counters": {"disk_read_bytes_per_second": 7}}]
		}`), nil
	})

	result, err := FetchProcesses(context.Background())
	if err != nil {
		t.Fatalf("FetchProcesses: %v", err)
	}
	if result.CapturedAtMs != 1700000000000 || result.Count.Total != 2 || len(result.Items) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.Items[0].PID != 42 || result.Items[0].IOCounters.DiskReadBytesPerSecond != 7 {
		t.Fatalf("process = %+v", result.Items[0])
	}
}

func TestFetchProgramsDecodesDaemonEnvelope(t *testing.T) {
	withTestAPIClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != programsRoute {
			t.Fatalf("path = %s, want %s", req.URL.Path, programsRoute)
		}
		return jsonResponse(http.StatusOK, `{
			"captured_at": 1700000000001,
			"items": [{"name": "worker", "count": 2, "cpu_percent": 25, "memory_percent": 3, "memory_rss_bytes": 16384, "pids": [41, 42]}]
		}`), nil
	})

	result, err := FetchPrograms(context.Background())
	if err != nil {
		t.Fatalf("FetchPrograms: %v", err)
	}
	if result.CapturedAtMs != 1700000000001 || len(result.Items) != 1 || result.Items[0].Name != "worker" {
		t.Fatalf("result = %+v", result)
	}
}

func TestMonitoringProcessHandlersReturnEmptyWhenDaemonUnavailable(t *testing.T) {
	withTestAPIClient(t, func(*http.Request) (*http.Response, error) {
		return nil, syscall.ECONNREFUSED
	})

	processes, err := handleGetProcesses(context.Background(), apischema.NoRequest{})
	if err != nil {
		t.Fatalf("handleGetProcesses error = %v", err)
	}
	if processes.Items == nil || len(processes.Items) != 0 {
		t.Fatalf("processes = %+v, want empty items", processes)
	}

	programs, err := handleGetPrograms(context.Background(), apischema.NoRequest{})
	if err != nil {
		t.Fatalf("handleGetPrograms error = %v", err)
	}
	if programs.Items == nil || len(programs.Items) != 0 {
		t.Fatalf("programs = %+v, want empty items", programs)
	}
}

func TestFetchProcessesHonorsCancellation(t *testing.T) {
	withTestAPIClient(t, func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := FetchProcesses(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
