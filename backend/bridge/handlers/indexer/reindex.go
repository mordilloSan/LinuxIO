package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
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

var errIndexerStreamEnded = errors.New("indexer stream ended unexpectedly")

const maxStatusStreamReattachAttempts = 3

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
	for attempt := 0; ; attempt++ {
		err := attachStatusStreamOnce(ctx, cb)
		if !errors.Is(err, errIndexerStreamEnded) {
			return err
		}

		reattach, statusErr := recoverEndedStatusStream(ctx, cb, attempt)
		if statusErr != nil {
			return statusErr
		}
		if !reattach {
			return nil
		}
	}
}

func attachStatusStreamOnce(ctx context.Context, cb IndexerCallbacks) error {
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

type indexerStatusSnapshot struct {
	Running      bool
	Status       string
	FilesIndexed int64
	DirsIndexed  int64
	TotalSize    int64
}

func fetchIndexerStatus(ctx context.Context) (indexerStatusSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/status", nil)
	if err != nil {
		return indexerStatusSnapshot{}, fmt.Errorf("create status request: %w", err)
	}

	resp, err := indexerClient.Do(req)
	if err != nil {
		return indexerStatusSnapshot{}, fmt.Errorf("indexer status request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return indexerStatusSnapshot{}, fmt.Errorf("indexer status: %s", resp.Status)
	}

	var raw struct {
		Status    string `json:"status"`
		NumDirs   int64  `json:"num_dirs"`
		NumFiles  int64  `json:"num_files"`
		TotalSize int64  `json:"total_size"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return indexerStatusSnapshot{}, fmt.Errorf("decode indexer status: %w", err)
	}

	status := strings.ToLower(strings.TrimSpace(raw.Status))
	return indexerStatusSnapshot{
		Running:      status == "running" || status == "indexing",
		Status:       status,
		FilesIndexed: raw.NumFiles,
		DirsIndexed:  raw.NumDirs,
		TotalSize:    raw.TotalSize,
	}, nil
}

func recoverEndedStatusStream(ctx context.Context, cb IndexerCallbacks, attempt int) (bool, error) {
	status, err := fetchIndexerStatus(ctx)
	if err != nil {
		if callbackErr := callOnError(cb, "indexer stream ended unexpectedly", 500); callbackErr != nil {
			return false, fmt.Errorf("on error callback: %w", callbackErr)
		}
		return false, errIndexerStreamEnded
	}

	if status.Running {
		if progressErr := callOnProgress(cb, IndexerProgress{
			FilesIndexed: status.FilesIndexed,
			DirsIndexed:  status.DirsIndexed,
			Phase:        "indexing",
		}); progressErr != nil {
			return false, fmt.Errorf("on progress callback: %w", progressErr)
		}
		if attempt >= maxStatusStreamReattachAttempts {
			if callbackErr := callOnError(cb, "indexer status stream kept closing while indexer was running", 500); callbackErr != nil {
				return false, fmt.Errorf("on error callback: %w", callbackErr)
			}
			return false, errIndexerStreamEnded
		}
		select {
		case <-time.After(500 * time.Millisecond):
			return true, nil
		case <-ctx.Done():
			return false, reportIndexerAbort(cb)
		}
	}

	if status.Status == "error" || status.Status == "failed" {
		if callbackErr := callOnError(cb, "indexer failed", 500); callbackErr != nil {
			return false, fmt.Errorf("on error callback: %w", callbackErr)
		}
		return false, fmt.Errorf("indexer failed")
	}

	if cb.OnResult != nil {
		if err := cb.OnResult(IndexerResult{
			Path:         "/",
			FilesIndexed: status.FilesIndexed,
			DirsIndexed:  status.DirsIndexed,
			TotalSize:    status.TotalSize,
		}); err != nil {
			return false, fmt.Errorf("on result callback: %w", err)
		}
	}
	return false, nil
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
		done, err := handleIndexerSSEEvent(cb, evt)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}

	if ctx.Err() != nil {
		return reportIndexerAbort(cb)
	}

	if err := <-errCh; err != nil {
		if ctx.Err() != nil {
			return reportIndexerAbort(cb)
		}
		if callbackErr := callOnError(cb, fmt.Sprintf("read error: %v", err), 500); callbackErr != nil {
			return fmt.Errorf("on error callback: %w", callbackErr)
		}
		return fmt.Errorf("read SSE: %w", err)
	}

	return errIndexerStreamEnded
}

func handleIndexerSSEEvent(cb IndexerCallbacks, evt SSEEvent) (bool, error) {
	switch evt.Type {
	case "started":
		return false, reportIndexerStart(cb)
	case "progress":
		return false, reportIndexerProgress(cb, evt.Data)
	case "complete":
		return reportIndexerComplete(cb, evt.Data)
	case "error":
		return false, reportIndexerError(cb, evt.Data)
	default:
		return false, nil
	}
}

func reportIndexerStart(cb IndexerCallbacks) error {
	if err := callOnProgress(cb, IndexerProgress{Phase: "indexing"}); err != nil {
		return fmt.Errorf("on progress callback: %w", err)
	}
	return nil
}

func reportIndexerProgress(cb IndexerCallbacks, data string) error {
	var progress IndexerProgress
	if err := json.Unmarshal([]byte(data), &progress); err != nil {
		return nil
	}
	progress.Phase = "indexing"
	if err := callOnProgress(cb, progress); err != nil {
		return fmt.Errorf("on progress callback: %w", err)
	}
	return nil
}

func reportIndexerComplete(cb IndexerCallbacks, data string) (bool, error) {
	var result IndexerResult
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		return false, nil
	}
	if cb.OnResult != nil {
		if err := cb.OnResult(result); err != nil {
			return false, fmt.Errorf("on result callback: %w", err)
		}
	}
	return true, nil
}

func reportIndexerError(cb IndexerCallbacks, data string) error {
	var errData struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(data), &errData); err != nil {
		return nil
	}
	if callbackErr := callOnError(cb, errData.Message, 500); callbackErr != nil {
		return fmt.Errorf("on error callback: %w", callbackErr)
	}
	return fmt.Errorf("indexer error: %s", errData.Message)
}

func reportIndexerAbort(cb IndexerCallbacks) error {
	if callbackErr := callOnError(cb, "operation aborted", 499); callbackErr != nil {
		return fmt.Errorf("on error callback: %w", callbackErr)
	}
	return ipc.ErrAborted
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
