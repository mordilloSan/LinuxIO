package monitoring

import (
	"context"
	"errors"
	"net/http"
	"syscall"
	"testing"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

func withTestAPIClient(t *testing.T, fn roundTripFunc) {
	t.Helper()
	orig := apiClient
	apiClient = &http.Client{Transport: fn}
	t.Cleanup(func() { apiClient = orig })
}

func TestFetchLiveDecodesPayload(t *testing.T) {
	withTestAPIClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != monitoringapi.RouteLive {
			t.Fatalf("path = %s", req.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{"captured_at_ms": 1700000000000, "cpu": {"percent": 3.5, "per_core_percent": [1, 6]}, "memory": {"total_bytes": 10}, "containers": {"captured_at_ms": 1700000000000, "items": []}}`), nil
	})
	live, err := FetchLive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if live.CapturedAtMs != 1700000000000 || live.CPU.PerCorePercent[1] != 6 || live.Memory.TotalBytes != 10 {
		t.Fatalf("live = %+v", live)
	}
}

func TestFetchLiveMapsDialFailureToUnavailable(t *testing.T) {
	withTestAPIClient(t, func(*http.Request) (*http.Response, error) { return nil, syscall.ECONNREFUSED })
	if _, err := FetchLive(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}
