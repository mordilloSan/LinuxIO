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
		return jsonResponse(http.StatusOK, `{ "index_path": "/", "index_name": "root", "include_hidden": true, "fts_search": true, "integrity_check": "quick" }`, nil), nil
	})

	cfg, err := FetchConfig(context.Background())
	if err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	if cfg.IndexPath != "/" || cfg.IndexName != "root" || !cfg.IncludeHidden || !cfg.FTSSearch || cfg.IntegrityCheck != "quick" {
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
