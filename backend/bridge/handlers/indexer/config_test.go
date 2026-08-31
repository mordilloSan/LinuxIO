package indexer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func withTestIndexerClient(t *testing.T, fn roundTripFunc) {
	t.Helper()
	orig := Client
	Client = &http.Client{Transport: fn}
	t.Cleanup(func() { Client = orig })
}

func withTestTimerInterval(t *testing.T, interval time.Duration) {
	t.Helper()
	original := getTimerInterval
	getTimerInterval = func(context.Context, string) (time.Duration, error) { return interval, nil }
	t.Cleanup(func() { getTimerInterval = original })
}

func TestSetConfigRouteIsPrivileged(t *testing.T) {
	for _, route := range Routes {
		if route.Route == "indexer.set_config" {
			if !route.Privileged {
				t.Fatal("indexer.set_config must remain privileged")
			}
			return
		}
	}
	t.Fatal("indexer.set_config route not registered")
}

func TestHandleGetConfigAddsTimerInterval(t *testing.T) {
	withTestIndexerClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Path != "/config" {
			t.Fatalf("request = %s %s", req.Method, req.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{"exclude_paths":["/home"],"include_network_mounts":false}`, nil), nil
	})
	withTestTimerInterval(t, 30*time.Minute)
	cfg, err := handleGetConfig(context.Background(), apischema.NoRequest{})
	if err != nil {
		t.Fatalf("handleGetConfig: %v", err)
	}
	if cfg.Interval != "30m0s" || cfg.IncludeNetworkMounts || !slices.Equal(cfg.ExcludePaths, []string{"/home"}) {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestUpdateConfigSendsReducedPatch(t *testing.T) {
	withTestIndexerClient(t, func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if got := string(body); got != `{"exclude_paths":["/home"],"include_network_mounts":false}` {
			t.Fatalf("body = %s", got)
		}
		return jsonResponse(http.StatusOK, `{"exclude_paths":["/home"],"include_network_mounts":false}`, nil), nil
	})
	cfg, err := UpdateConfig(context.Background(), []byte(`{"exclude_paths":["/home"],"include_network_mounts":false}`))
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if !slices.Equal(cfg.ExcludePaths, []string{"/home"}) || cfg.IncludeNetworkMounts {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestNormalizeConfigPatchRejectsRemovedAndInvalidFields(t *testing.T) {
	for _, payload := range [][]byte{
		[]byte(`{"db_path":"/tmp/index.db"}`),
		[]byte(`{"listen_addr":":8080"}`),
		[]byte(`{"exclude_paths":[],"exclude_paths":["/data"]}`),
		[]byte(`{"unknown":true}`),
	} {
		if _, err := normalizeConfigPatchPayload(payload); err == nil {
			t.Fatalf("accepted payload %s", payload)
		}
	}
}

func TestFetchConfigPreservesHTTPResponseError(t *testing.T) {
	withTestIndexerClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusConflict, "another update is active", nil), nil
	})
	_, err := FetchConfig(context.Background())
	var responseErr *ResponseError
	if !errors.As(err, &responseErr) || responseErr.StatusCode != http.StatusConflict {
		t.Fatalf("error = %v", err)
	}
}

func jsonResponse(status int, body string, header http.Header) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
