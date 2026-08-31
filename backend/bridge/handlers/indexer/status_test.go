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
			"protocol_version": 1,
			"status": "indexing",
			"fts_active": true,
			"num_dirs": 12,
			"num_files": 345,
			"total_size": 4096,
			"last_indexed": "2026-05-06T10:30:00Z",
			"total_indexes": 2,
			"total_entries": 357,
			"database_size": 1048576,
			"wal_size": 2048,
			"shm_size": 32768,
			"total_on_disk": 1081344
		}`, nil), nil
	})

	status, err := FetchStatus(context.Background())
	if err != nil {
		t.Fatalf("FetchStatus: %v", err)
	}
	if !status.Running || status.Status != "indexing" {
		t.Fatalf("status running=%v status=%q, want running indexing", status.Running, status.Status)
	}
	if status.NumFiles != 345 || status.NumDirs != 12 || status.TotalEntries != 357 {
		t.Fatalf("unexpected counts: %#v", status)
	}
	if status.DatabaseSize != 1048576 || status.TotalOnDisk != 1081344 {
		t.Fatalf("unexpected storage counters: %#v", status)
	}
	if !status.FTSActive {
		t.Fatalf("fts_active = false, want true")
	}
}

func TestFetchStatusRejectsProtocolMismatch(t *testing.T) {
	withTestIndexerClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"protocol_version":2,"status":"idle"}`, nil), nil
	})

	if _, err := FetchStatus(context.Background()); err == nil {
		t.Fatal("FetchStatus accepted an incompatible protocol")
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
