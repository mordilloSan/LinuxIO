package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// indexerClient is the HTTP client for SSE connections to the indexer service.
// Unexported — only used by StreamReindex.
var indexerClient = &http.Client{
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			return dialer.DialContext(ctx, "unix", "/var/run/indexer.sock")
		},
	},
}

// ReindexCallbacks defines how reindex events are delivered to the caller.
// Nil callbacks are safely skipped (no-op).
type ReindexCallbacks struct {
	OnProgress func(ReindexProgress) error
	OnResult   func(ReindexResult) error
	OnError    func(msg string, code int) error
}

// StreamReindex connects to the indexer SSE endpoint for the given path and
// relays events via callbacks. The caller controls cancellation through ctx
// (e.g. via ipc.AbortContext).
//
// HTTP status-to-error mapping is centralized here so handler wrappers stay thin.
func StreamReindex(ctx context.Context, path string, cb ReindexCallbacks) error {
	query := url.Values{}
	query.Set("path", path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/reindex/stream?"+query.Encode(), nil)
	if err != nil {
		if callbackErr := callOnError(cb, fmt.Sprintf("failed to create request: %v", err), 500); callbackErr != nil {
			return fmt.Errorf("on error callback: %w", callbackErr)
		}
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	// Send initial "connecting" progress
	if progressErr := callOnProgress(cb, ReindexProgress{Phase: "connecting"}); progressErr != nil {
		return fmt.Errorf("on progress callback: %w", progressErr)
	}

	resp, err := indexerClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			if callbackErr := callOnError(cb, "operation aborted", 499); callbackErr != nil {
				return fmt.Errorf("on error callback: %w", callbackErr)
			}
			return ipc.ErrAborted
		}
		if callbackErr := callOnError(cb, fmt.Sprintf("indexer connection failed: %v", err), 503); callbackErr != nil {
			return fmt.Errorf("on error callback: %w", callbackErr)
		}
		return fmt.Errorf("indexer request: %w", err)
	}
	defer resp.Body.Close()

	// Centralized HTTP status mapping
	switch {
	case resp.StatusCode == http.StatusConflict:
		if callbackErr := callOnError(cb, "another index operation is already running", 409); callbackErr != nil {
			return fmt.Errorf("on error callback: %w", callbackErr)
		}
		return fmt.Errorf("indexer conflict")
	case resp.StatusCode == http.StatusBadRequest:
		if callbackErr := callOnError(cb, "invalid path", 400); callbackErr != nil {
			return fmt.Errorf("on error callback: %w", callbackErr)
		}
		return fmt.Errorf("invalid path")
	case resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK:
		if callbackErr := callOnError(cb, fmt.Sprintf("indexer error: %s", resp.Status), resp.StatusCode); callbackErr != nil {
			return fmt.Errorf("on error callback: %w", callbackErr)
		}
		return fmt.Errorf("indexer error: %s", resp.Status)
	}

	// Parse SSE events
	events, errCh := ReadSSE(ctx, resp.Body)

	for evt := range events {
		switch evt.Type {
		case "started":
			if progressErr := callOnProgress(cb, ReindexProgress{Phase: "indexing"}); progressErr != nil {
				return fmt.Errorf("on progress callback: %w", progressErr)
			}

		case "progress":
			var progress ReindexProgress
			if unmarshalErr := json.Unmarshal([]byte(evt.Data), &progress); unmarshalErr == nil {
				progress.Phase = "indexing"
				if callbackErr := callOnProgress(cb, progress); callbackErr != nil {
					return fmt.Errorf("on progress callback: %w", callbackErr)
				}
			}

		case "complete":
			var result ReindexResult
			if unmarshalErr := json.Unmarshal([]byte(evt.Data), &result); unmarshalErr == nil {
				if cb.OnResult != nil {
					if callbackErr := cb.OnResult(result); callbackErr != nil {
						return fmt.Errorf("on result callback: %w", callbackErr)
					}
				}
				return nil
			}

		case "error":
			var errData struct {
				Message string `json:"message"`
			}
			if unmarshalErr := json.Unmarshal([]byte(evt.Data), &errData); unmarshalErr == nil {
				if callbackErr := callOnError(cb, errData.Message, 500); callbackErr != nil {
					return fmt.Errorf("on error callback: %w", callbackErr)
				}
				return fmt.Errorf("indexer error: %s", errData.Message)
			}
		}
	}

	// Check for SSE read error
	if err := <-errCh; err != nil {
		if ctx.Err() != nil {
			if callbackErr := callOnError(cb, "operation aborted", 499); callbackErr != nil {
				return fmt.Errorf("on error callback: %w", callbackErr)
			}
			return ipc.ErrAborted
		}
		if callbackErr := callOnError(cb, fmt.Sprintf("read error: %v", err), 500); callbackErr != nil {
			return fmt.Errorf("on error callback: %w", callbackErr)
		}
		return fmt.Errorf("read SSE: %w", err)
	}

	// Stream ended without a "complete" event
	if err := callOnError(cb, "indexer stream ended unexpectedly", 500); err != nil {
		return fmt.Errorf("on error callback: %w", err)
	}
	return fmt.Errorf("indexer stream ended unexpectedly")
}

func callOnProgress(cb ReindexCallbacks, p ReindexProgress) error {
	if cb.OnProgress != nil {
		return cb.OnProgress(p)
	}
	return nil
}

func callOnError(cb ReindexCallbacks, msg string, code int) error {
	if cb.OnError != nil {
		return cb.OnError(msg, code)
	}
	return nil
}
