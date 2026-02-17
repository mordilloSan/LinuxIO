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
// Unexported — only used by StreamIndexer and StreamIndexerAttach.
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

// IndexerCallbacks defines how indexer events are delivered to the caller.
// Nil callbacks are safely skipped (no-op).
type IndexerCallbacks struct {
	OnProgress func(IndexerProgress) error
	OnResult   func(IndexerResult) error
	OnError    func(msg string, code int) error
}

// StreamIndexer triggers a reindex via POST /reindex?path=<path> and then
// attaches to GET /status?stream=true for live SSE updates. The caller
// controls cancellation through ctx (e.g. via ipc.AbortContext).
//
// HTTP status-to-error mapping is centralized here so handler wrappers stay thin.
func StreamIndexer(ctx context.Context, path string, cb IndexerCallbacks) error {
	// Send initial "connecting" progress
	if progressErr := callOnProgress(cb, IndexerProgress{Phase: "connecting"}); progressErr != nil {
		return fmt.Errorf("on progress callback: %w", progressErr)
	}

	// Step 1: Trigger the reindex operation
	if err := triggerReindex(ctx, path, cb); err != nil {
		return err
	}

	// Step 2: Attach to the status stream for live SSE events
	return attachStatusStream(ctx, cb)
}

// triggerReindex sends POST /reindex?path=<path> to start the operation.
func triggerReindex(ctx context.Context, path string, cb IndexerCallbacks) error {
	query := url.Values{}
	query.Set("path", path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/reindex?"+query.Encode(), nil)
	if err != nil {
		if callbackErr := callOnError(cb, fmt.Sprintf("failed to create request: %v", err), 500); callbackErr != nil {
			return fmt.Errorf("on error callback: %w", callbackErr)
		}
		return fmt.Errorf("create request: %w", err)
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

	return nil
}

// attachStatusStream connects to GET /status?stream=true for live SSE events.
func attachStatusStream(ctx context.Context, cb IndexerCallbacks) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/status?stream=true", nil)
	if err != nil {
		if callbackErr := callOnError(cb, fmt.Sprintf("failed to create request: %v", err), 500); callbackErr != nil {
			return fmt.Errorf("on error callback: %w", callbackErr)
		}
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

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

	if resp.StatusCode != http.StatusOK {
		if callbackErr := callOnError(cb, fmt.Sprintf("indexer status stream error: %s", resp.Status), resp.StatusCode); callbackErr != nil {
			return fmt.Errorf("on error callback: %w", callbackErr)
		}
		return fmt.Errorf("indexer status stream: %s", resp.Status)
	}

	return consumeSSEEvents(ctx, resp, cb)
}

// StreamIndexerAttach connects to the indexer status SSE endpoint to attach
// to an already-running operation. Uses GET /status?stream=true which streams
// the same SSE events (started, progress, complete, error) as StreamIndexer.
//
// Returns an error if no operation is currently running or the connection fails.
func StreamIndexerAttach(ctx context.Context, cb IndexerCallbacks) error {
	// Send initial "connecting" progress
	if progressErr := callOnProgress(cb, IndexerProgress{Phase: "connecting"}); progressErr != nil {
		return fmt.Errorf("on progress callback: %w", progressErr)
	}

	return attachStatusStream(ctx, cb)
}

// consumeSSEEvents reads SSE events from an HTTP response and dispatches them
// via the provided callbacks. Shared by StreamIndexer and StreamIndexerAttach.
func consumeSSEEvents(ctx context.Context, resp *http.Response, cb IndexerCallbacks) error {
	events, errCh := ReadSSE(ctx, resp.Body)

	for evt := range events {
		switch evt.Type {
		case "started":
			if progressErr := callOnProgress(cb, IndexerProgress{Phase: "indexing"}); progressErr != nil {
				return fmt.Errorf("on progress callback: %w", progressErr)
			}

		case "progress":
			var progress IndexerProgress
			if unmarshalErr := json.Unmarshal([]byte(evt.Data), &progress); unmarshalErr == nil {
				progress.Phase = "indexing"
				if callbackErr := callOnProgress(cb, progress); callbackErr != nil {
					return fmt.Errorf("on progress callback: %w", callbackErr)
				}
			}

		case "complete":
			var result IndexerResult
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

	// Check for context cancellation first
	if ctx.Err() != nil {
		if callbackErr := callOnError(cb, "operation aborted", 499); callbackErr != nil {
			return fmt.Errorf("on error callback: %w", callbackErr)
		}
		return ipc.ErrAborted
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

func callOnProgress(cb IndexerCallbacks, p IndexerProgress) error {
	if cb.OnProgress != nil {
		return cb.OnProgress(p)
	}
	return nil
}

func callOnError(cb IndexerCallbacks, msg string, code int) error {
	if cb.OnError != nil {
		return cb.OnError(msg, code)
	}
	return nil
}
