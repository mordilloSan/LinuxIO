package indexer

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFetchStatusReadsDaemonCounters(t *testing.T) {
	withTestIndexerClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", req.Method)
		}
		if req.URL.Path != "/status" {
			t.Fatalf("path = %s, want /status", req.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{
			"status": "indexing",
			"num_dirs": 12,
			"num_files": 345,
			"total_size": 4096,
			"last_indexed": "2026-05-06T10:30:00Z",
			"database_size": 1048576
		}`, nil), nil
	})

	status, err := FetchStatus(context.Background())
	if err != nil {
		t.Fatalf("FetchStatus: %v", err)
	}
	if !status.Running || status.Status != "indexing" {
		t.Fatalf("status running=%v status=%q, want running indexing", status.Running, status.Status)
	}
	if status.NumFiles != 345 || status.NumDirs != 12 {
		t.Fatalf("unexpected counts: %#v", status)
	}
	if status.DatabaseSize != 1048576 {
		t.Fatalf("unexpected storage counters: %#v", status)
	}
}

func TestFetchStatusBoundsResponseAndPreservesCancellation(t *testing.T) {
	t.Run("oversized", func(t *testing.T) {
		withTestIndexerClient(t, func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", maxIndexerResponseBytes+1))),
			}, nil
		})
		if _, err := FetchStatus(context.Background()); err == nil {
			t.Fatal("FetchStatus accepted an oversized response")
		}
	})

	t.Run("canceled", func(t *testing.T) {
		withTestIndexerClient(t, func(req *http.Request) (*http.Response, error) {
			return nil, req.Context().Err()
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := FetchStatus(ctx); err != context.Canceled {
			t.Fatalf("FetchStatus error = %v, want context.Canceled", err)
		}
	})
}
