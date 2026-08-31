package indexer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/indexer/systemdunit"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

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

func withTestTCPListener(t *testing.T, address string) {
	t.Helper()
	original := currentTCPListener
	currentTCPListener = func(context.Context) (string, error) { return address, nil }
	t.Cleanup(func() { currentTCPListener = original })
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

func TestFetchConfigUsesUnixConfigEndpoint(t *testing.T) {
	withTestIndexerClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", req.Method)
		}
		if req.URL.Path != "/config" {
			t.Fatalf("path = %s, want /config", req.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{ "index_path": "/", "index_name": "root", "exclude_paths": ["/proc", "/dev"], "include_hidden": true, "fts_search": true, "integrity_check": "quick" }`, nil), nil
	})

	cfg, err := FetchConfig(context.Background())
	if err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	if cfg.IndexPath != "/" || cfg.IndexName != "root" || !slices.Equal(cfg.ExcludePaths, []string{"/proc", "/dev"}) || !cfg.IncludeHidden || !cfg.FTSSearch || cfg.IntegrityCheck != "quick" {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestHandleGetConfigAddsSystemdTimerInterval(t *testing.T) {
	withTestIndexerClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"index_path":"/","index_name":"root"}`, nil), nil
	})
	withTestTimerInterval(t, 30*time.Minute)
	withTestTCPListener(t, ":8080")

	cfg, err := handleGetConfig(context.Background(), apischema.NoRequest{})
	if err != nil {
		t.Fatalf("handleGetConfig: %v", err)
	}
	if cfg.IndexPath != "/" || cfg.Interval != "30m0s" || cfg.ListenAddr != ":8080" {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestUpdateConfigSendsTypedPatchAndReadsRestartHeader(t *testing.T) {
	withTestIndexerClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", req.Method)
		}
		if got := req.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type = %q", got)
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		got := string(body)
		if !strings.Contains(got, `"index_path":"/data"`) {
			t.Fatalf("body missing index_path: %s", got)
		}
		if !strings.Contains(got, `"include_hidden":false`) {
			t.Fatalf("body missing explicit false: %s", got)
		}
		if !strings.Contains(got, `"fts_search":false`) {
			t.Fatalf("body missing explicit fts_search false: %s", got)
		}
		if strings.Contains(got, `"db_path":null`) {
			t.Fatalf("body included null fields: %s", got)
		}

		header := http.Header{"X-Indexer-Restart-Required": []string{"true"}}
		return jsonResponse(http.StatusOK, `{ "index_path": "/data", "include_hidden": false, "fts_search": false }`, header), nil
	})

	cfg, restartRequired, err := UpdateConfig(
		context.Background(),
		[]byte(`{"index_path":"/data","include_hidden":false,"fts_search":false}`),
	)
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if !restartRequired {
		t.Fatal("restartRequired = false, want true")
	}
	if cfg.IndexPath != "/data" || cfg.IncludeHidden || cfg.FTSSearch {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestHandleSetConfigAppliesTCPListener(t *testing.T) {
	withTestTimerInterval(t, time.Hour)
	withTestTCPListener(t, "")
	withTestIndexerClient(t, func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if got := string(body); got != `{}` {
			t.Fatalf("daemon config body = %s, want listener omitted", got)
		}
		return jsonResponse(http.StatusOK, `{ "index_path": "/" }`, nil), nil
	})
	original := configureTCPListener
	t.Cleanup(func() { configureTCPListener = original })
	var got string
	configureTCPListener = func(_ context.Context, listenAddr string) error {
		got = listenAddr
		return nil
	}

	listenAddr := ":8080"
	result, err := handleSetConfig(context.Background(), apischema.IndexerConfigPatch{ListenAddr: &listenAddr})
	if err != nil {
		t.Fatalf("handleSetConfig: %v", err)
	}
	if got != listenAddr {
		t.Fatalf("configured address = %q, want %q", got, listenAddr)
	}
	if !result.RestartRequired || result.Config.ListenAddr != listenAddr {
		t.Fatalf("result = %#v", result)
	}
}

func TestCurrentTCPListenerReadsSystemdSocket(t *testing.T) {
	originalPath := tcpSocketUnitPath
	originalGet := getTCPSocketAddress
	t.Cleanup(func() {
		tcpSocketUnitPath = originalPath
		getTCPSocketAddress = originalGet
	})

	tcpSocketUnitPath = filepath.Join(t.TempDir(), systemdunit.TCPSocketUnitName)
	if err := os.WriteFile(tcpSocketUnitPath, []byte("[Socket]\nListenStream=:8080\n"), 0o644); err != nil {
		t.Fatalf("write socket unit: %v", err)
	}
	getTCPSocketAddress = func(_ context.Context, unit string) (string, error) {
		if unit != systemdunit.TCPSocketUnitName {
			t.Fatalf("unit = %q", unit)
		}
		return "[::]:8080", nil
	}

	address, err := CurrentTCPListener(context.Background())
	if err != nil {
		t.Fatalf("CurrentTCPListener: %v", err)
	}
	if address != "[::]:8080" {
		t.Fatalf("address = %q", address)
	}
}

func TestConfigureTCPListenerWritesAndRemovesUnit(t *testing.T) {
	originalPath := tcpSocketUnitPath
	originalEnable := enableTCPSocketUnit
	originalDisable := disableTCPSocketUnit
	originalStop := stopTCPSocketUnit
	originalRestart := restartTCPSocketUnit
	originalReload := reloadTCPSystemd
	t.Cleanup(func() {
		tcpSocketUnitPath = originalPath
		enableTCPSocketUnit = originalEnable
		disableTCPSocketUnit = originalDisable
		stopTCPSocketUnit = originalStop
		restartTCPSocketUnit = originalRestart
		reloadTCPSystemd = originalReload
	})

	tcpSocketUnitPath = filepath.Join(t.TempDir(), systemdunit.TCPSocketUnitName)
	var calls []string
	enableTCPSocketUnit = func(context.Context, string) error { calls = append(calls, "enable"); return nil }
	disableTCPSocketUnit = func(context.Context, string) error { calls = append(calls, "disable"); return nil }
	stopTCPSocketUnit = func(context.Context, string) error { calls = append(calls, "stop"); return nil }
	restartTCPSocketUnit = func(context.Context, string) error { calls = append(calls, "restart"); return nil }
	reloadTCPSystemd = func(context.Context) error { calls = append(calls, "reload"); return nil }

	if err := ConfigureTCPListener(context.Background(), ":8080"); err != nil {
		t.Fatalf("enable TCP listener: %v", err)
	}
	unit, err := os.ReadFile(tcpSocketUnitPath)
	if err != nil {
		t.Fatalf("read TCP socket unit: %v", err)
	}
	if !strings.Contains(string(unit), "ListenStream=:8080\n") {
		t.Fatalf("unexpected TCP socket unit:\n%s", unit)
	}
	if err := ConfigureTCPListener(context.Background(), ""); err != nil {
		t.Fatalf("disable TCP listener: %v", err)
	}
	if _, err := os.Stat(tcpSocketUnitPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("TCP socket unit still exists: %v", err)
	}
	if got := strings.Join(calls, ","); got != "enable,restart,disable,stop,reload" {
		t.Fatalf("systemd calls = %q", got)
	}
}

func TestUpdateConfigFTSSearchDoesNotRequireRestart(t *testing.T) {
	withTestIndexerClient(t, func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if got := string(body); got != `{"fts_search":false}` {
			t.Fatalf("body = %s, want fts_search patch", got)
		}
		return jsonResponse(http.StatusOK, `{ "fts_search": false }`, nil), nil
	})

	cfg, restartRequired, err := UpdateConfig(
		context.Background(),
		[]byte(`{"fts_search":false}`),
	)
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if restartRequired {
		t.Fatal("restartRequired = true, want false")
	}
	if cfg.FTSSearch {
		t.Fatalf("fts_search = true, want false")
	}
}

func TestUpdateConfigIntegrityCheckDoesNotRequireRestart(t *testing.T) {
	withTestIndexerClient(t, func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if got := string(body); got != `{"integrity_check":"off"}` {
			t.Fatalf("body = %s, want integrity_check patch", got)
		}
		return jsonResponse(http.StatusOK, `{ "integrity_check": "off" }`, nil), nil
	})

	cfg, restartRequired, err := UpdateConfig(
		context.Background(),
		[]byte(`{"integrity_check":"off"}`),
	)
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if restartRequired {
		t.Fatal("restartRequired = true, want false")
	}
	if cfg.IntegrityCheck != "off" {
		t.Fatalf("integrity_check = %q, want off", cfg.IntegrityCheck)
	}
}

func TestNormalizeConfigPatchRejectsNonCanonicalJSON(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{name: "unknown member", payload: []byte(`{"unknown":true}`)},
		{name: "case-mismatched member", payload: []byte(`{"FTSSearch":false}`)},
		{name: "duplicate member", payload: []byte(`{"fts_search":true,"fts_search":false}`)},
		{name: "invalid UTF-8", payload: []byte("{\"index_path\":\"\xff\"}")},
		{name: "trailing value", payload: []byte(`{"fts_search":false} {}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := normalizeConfigPatchPayload(tt.payload); err == nil {
				t.Fatal("expected invalid JSON error")
			}
		})
	}
}

func TestFetchConfigPreservesHTTPResponseError(t *testing.T) {
	withTestIndexerClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusConflict, "another update is active", nil), nil
	})

	_, err := FetchConfig(context.Background())
	var responseErr *ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("error = %v, want ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusConflict || responseErr.Route != "/config" {
		t.Fatalf("response error = %#v", responseErr)
	}
}

func TestUpdateConfigPreservesUnavailableResponseError(t *testing.T) {
	withTestIndexerClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, "database unavailable", nil), nil
	})

	_, _, err := UpdateConfig(context.Background(), []byte(`{"fts_search":false}`))
	var responseErr *ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("error = %v, want ResponseError", err)
	}
	if !errors.Is(err, ErrUnavailable) || responseErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("error = %v, response error = %#v", err, responseErr)
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
