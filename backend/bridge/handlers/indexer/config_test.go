package indexer

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func withTestIndexerClient(t *testing.T, fn roundTripFunc) {
	t.Helper()
	orig := indexerClient
	indexerClient = &http.Client{Transport: fn}
	t.Cleanup(func() { indexerClient = orig })
}

func TestFetchConfigUsesUnixConfigEndpoint(t *testing.T) {
	withTestIndexerClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", req.Method)
		}
		if req.URL.Path != "/config" {
			t.Fatalf("path = %s, want /config", req.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{ "index_path": "/", "index_name": "root", "include_hidden": true }`, nil), nil
	})

	cfg, err := FetchConfig(context.Background())
	if err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	if cfg.IndexPath != "/" || cfg.IndexName != "root" || !cfg.IncludeHidden {
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
		if strings.Contains(got, `"db_path":null`) {
			t.Fatalf("body included null fields: %s", got)
		}

		header := http.Header{"X-Indexer-Restart-Required": []string{"true"}}
		return jsonResponse(http.StatusOK, `{ "index_path": "/data", "include_hidden": false }`, header), nil
	})

	cfg, restartRequired, err := UpdateConfig(
		context.Background(),
		[]byte(`{"index_path":"/data","include_hidden":false}`),
	)
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if !restartRequired {
		t.Fatal("restartRequired = false, want true")
	}
	if cfg.IndexPath != "/data" || cfg.IncludeHidden {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestNormalizeConfigPatchRejectsUnknownFields(t *testing.T) {
	_, err := normalizeConfigPatchPayload([]byte(`{"unknown":true}`))
	if err == nil {
		t.Fatal("expected unknown field error")
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
