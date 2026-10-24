package monitoring

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"syscall"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
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

func TestMonitoringLiveHandlerReturnsZeroWhenDaemonUnavailable(t *testing.T) {
	withTestAPIClient(t, func(*http.Request) (*http.Response, error) { return nil, syscall.ECONNREFUSED })

	live, err := handleGetLive(context.Background(), apischema.NoRequest{})
	if err != nil {
		t.Fatalf("handleGetLive error = %v", err)
	}
	if live.CapturedAtMs != 0 || live.CPU.Percent != 0 || live.Memory.TotalBytes != 0 {
		t.Fatalf("live = %+v, want zero payload", live)
	}
}

func TestGetAPIBoundsReplySize(t *testing.T) {
	const limit = 16
	body := `{"a":"` + strings.Repeat("x", limit-8) + `"}` // exactly limit bytes
	payload := body
	withTestAPIClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, payload), nil
	})

	var out map[string]string
	if err := getAPI(context.Background(), "/x", limit, &out); err != nil {
		t.Fatalf("limit-sized reply: %v", err)
	}

	payload = body + " "
	err := getAPI(context.Background(), "/x", limit, &out)
	if !errors.Is(err, errOversize) || errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "exceeds 16 bytes") {
		t.Fatalf("oversized reply err = %v, want exceeds 16 bytes without ErrUnavailable", err)
	}
}
