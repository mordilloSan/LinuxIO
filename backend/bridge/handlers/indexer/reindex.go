package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

var errIndexerStreamEnded = errors.New("indexer stream ended unexpectedly")

const maxStatusStreamReattachAttempts = 3

// IndexerCallbacks defines how indexer events are delivered to the caller.
// Nil callbacks are safely skipped (no-op).
type IndexerCallbacks struct {
	OnProgress func(IndexerProgress) error
	OnResult   func(IndexerResult) error
	OnError    func(msg string, code int) error
}

// StreamIndexer triggers a full index for / or a path reindex for a subpath,
// then attaches to GET /status?stream=true for live SSE updates. The caller
// controls cancellation through ctx.
//
// HTTP status-to-error mapping is centralized here so handler wrappers stay thin.
func StreamIndexer(ctx context.Context, path string, cb IndexerCallbacks) error {
	// Send initial "connecting" progress
	if progressErr := callOnProgress(cb, IndexerProgress{
		Phase:     "connecting",
		State:     "connecting",
		Operation: indexerOperationForPath(path),
	}); progressErr != nil {
		return fmt.Errorf("on progress callback: %w", progressErr)
	}

	// Step 1: Trigger the requested operation.
	operationID, err := triggerIndexer(ctx, path, cb)
	if err != nil {
		return err
	}

	// Step 2: Attach to the status stream for live SSE events
	return attachStatusStream(ctx, cb, streamExpectation(path, operationID))
}

func indexerOperationForPath(path string) string {
	if path == "" || path == "/" {
		return "index"
	}
	return "reindex"
}

func isIndexerOperation(operation string) bool {
	return operation == "index" || operation == "reindex"
}

func triggerIndexerError(cb IndexerCallbacks, message string, code int, err error) (string, error) {
	if callbackErr := callOnError(cb, message, code); callbackErr != nil {
		return "", fmt.Errorf("on error callback: %w", callbackErr)
	}
	return "", err
}

func validateTriggerResponse(cb IndexerCallbacks, resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusConflict:
		_, err := triggerIndexerError(cb, "another index operation is already running", 409, errors.New("indexer conflict"))
		return err
	case http.StatusBadRequest:
		_, err := triggerIndexerError(cb, "invalid path", 400, errors.New("invalid path"))
		return err
	case http.StatusAccepted, http.StatusOK:
		return nil
	default:
		_, err := triggerIndexerError(cb, fmt.Sprintf("indexer error: %s", resp.Status), resp.StatusCode, fmt.Errorf("indexer error: %s", resp.Status))
		return err
	}
}

// triggerIndexer sends POST /index for a full run or POST /reindex?path= for
// a subpath.
func triggerIndexer(ctx context.Context, path string, cb IndexerCallbacks) (string, error) {
	endpoint := "http://unix" + indexerapi.RouteIndex
	if indexerOperationForPath(path) == "reindex" {
		query := url.Values{}
		query.Set("path", path)
		endpoint = "http://unix" + indexerapi.RouteReindex + "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return triggerIndexerError(cb, fmt.Sprintf("failed to create request: %v", err), 500, fmt.Errorf("create request: %w", err))
	}

	resp, err := Client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return triggerIndexerError(cb, "operation aborted", 499, ipc.ErrAborted)
		}
		return triggerIndexerError(cb, fmt.Sprintf("indexer connection failed: %v", err), 503, fmt.Errorf("indexer request: %w", err))
	}
	defer resp.Body.Close()

	err = validateTriggerResponse(cb, resp)
	if err != nil {
		return "", err
	}

	payload, err := readBoundedBody(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read indexer trigger response: %w", err)
	}
	if len(strings.TrimSpace(string(payload))) == 0 {
		return "", errors.New("indexer trigger response is empty")
	}
	var result indexerapi.OperationResponse
	if err := json.Unmarshal(payload, &result); err != nil {
		return "", fmt.Errorf("decode indexer trigger response: %w", err)
	}
	if strings.TrimSpace(result.OperationID) == "" {
		return "", errors.New("indexer trigger response is missing operation_id")
	}
	return strings.TrimSpace(result.OperationID), nil
}

// attachStatusStream connects to GET /status?stream=true for live SSE events.
type indexerStreamExpectation struct {
	operation   string
	operationID string
	path        string
}

func streamExpectation(path, operationID string) indexerStreamExpectation {
	if path == "" {
		path = "/"
	}
	return indexerStreamExpectation{operation: indexerOperationForPath(path), operationID: operationID, path: path}
}

func attachStatusStream(ctx context.Context, cb IndexerCallbacks, expected indexerStreamExpectation) error {
	for attempt := 0; ; attempt++ {
		err := attachStatusStreamOnce(ctx, cb, &expected)
		if !errors.Is(err, errIndexerStreamEnded) {
			return err
		}

		reattach, statusErr := recoverEndedStatusStream(ctx, cb, &expected, attempt)
		if statusErr != nil {
			return statusErr
		}
		if !reattach {
			return nil
		}
	}
}

func attachStatusStreamOnce(ctx context.Context, cb IndexerCallbacks, expected *indexerStreamExpectation) error {
	if expected == nil || expected.operation == "" || expected.operationID == "" || expected.path == "" {
		return reportIndexerIdentityError(cb, "indexer operation identity is incomplete")
	}
	query := url.Values{
		"stream":       {"true"},
		"operation":    {expected.operation},
		"operation_id": {expected.operationID},
		"path":         {expected.path},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix"+indexerapi.RouteStatus+"?"+query.Encode(), nil)
	if err != nil {
		if callbackErr := callOnError(cb, fmt.Sprintf("failed to create request: %v", err), 500); callbackErr != nil {
			return fmt.Errorf("on error callback: %w", callbackErr)
		}
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := Client.Do(req)
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

	return consumeSSEEvents(ctx, resp, cb, expected)
}

type indexerStatusSnapshot struct {
	Running      bool
	Status       string
	Operation    string
	Path         string
	OperationID  string
	FilesIndexed int64
	DirsIndexed  int64
	TotalSize    int64
}

func fetchIndexerStatus(ctx context.Context) (indexerStatusSnapshot, error) {
	status, err := FetchStatus(ctx)
	if err != nil {
		return indexerStatusSnapshot{}, err
	}
	return indexerStatusSnapshot{
		Running:      status.Running,
		Status:       status.Status,
		Operation:    status.ActiveOp,
		OperationID:  status.OperationID,
		Path:         status.ActivePath,
		FilesIndexed: status.NumFiles,
		DirsIndexed:  status.NumDirs,
		TotalSize:    status.TotalSize,
	}, nil
}

//nolint:gocognit // Recovery keeps callback and terminal-state handling in one lifecycle.
func recoverEndedStatusStream(ctx context.Context, cb IndexerCallbacks, expected *indexerStreamExpectation, attempt int) (bool, error) {
	status, err := fetchIndexerStatus(ctx)
	if err != nil {
		if callbackErr := callOnError(cb, "indexer stream ended unexpectedly", 500); callbackErr != nil {
			return false, fmt.Errorf("on error callback: %w", callbackErr)
		}
		return false, errIndexerStreamEnded
	}

	if status.Running {
		if err := validateActiveIndexerStatus(cb, expected, status); err != nil {
			return false, err
		}
		if status.Path == "" {
			status.Path = expected.path
		}
		if err := reportRecoveredIndexerProgress(cb, status); err != nil {
			return false, err
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
			Operation:    expected.operation,
			OperationID:  expected.operationID,
			Path:         expected.path,
			FilesIndexed: status.FilesIndexed,
			DirsIndexed:  status.DirsIndexed,
			TotalSize:    status.TotalSize,
		}); err != nil {
			return false, fmt.Errorf("on result callback: %w", err)
		}
	}
	return false, nil
}

func reportRecoveredIndexerProgress(cb IndexerCallbacks, status indexerStatusSnapshot) error {
	if err := validateIndexerOperation(cb, status.Operation); err != nil {
		return err
	}

	progress := IndexerProgress{
		FilesIndexed: status.FilesIndexed,
		DirsIndexed:  status.DirsIndexed,
		Operation:    status.Operation,
		OperationID:  status.OperationID,
		CurrentPath:  status.Path,
		State:        status.Status,
	}
	normalizeIndexerProgress(&progress)
	if progressErr := callOnProgress(cb, progress); progressErr != nil {
		return fmt.Errorf("on progress callback: %w", progressErr)
	}
	return nil
}

// StreamIndexerAttach connects to the indexer status SSE endpoint to attach
// to an already-running operation. Uses GET /status?stream=true which streams
// the same SSE events (started, progress, complete, error) as StreamIndexer.
//
// Returns an error if no operation is currently running or the connection fails.
func StreamIndexerAttach(ctx context.Context, path string, cb IndexerCallbacks) error {
	// Send initial "connecting" progress
	if progressErr := callOnProgress(cb, IndexerProgress{
		Phase: "connecting",
		State: "connecting",
	}); progressErr != nil {
		return fmt.Errorf("on progress callback: %w", progressErr)
	}

	status, err := fetchIndexerStatus(ctx)
	if err != nil {
		if responseErr, ok := errors.AsType[*ResponseError](err); ok {
			if callbackErr := callOnError(cb, responseErr.Message, responseErr.StatusCode); callbackErr != nil {
				return fmt.Errorf("on error callback: %w", callbackErr)
			}
		}
		return err
	}
	if !status.Running || status.OperationID == "" {
		message := "no active indexer operation"
		if callbackErr := callOnError(cb, message, http.StatusConflict); callbackErr != nil {
			return fmt.Errorf("on error callback: %w", callbackErr)
		}
		return errors.New(message)
	}
	expected := streamExpectation(path, status.OperationID)
	if err := validateActiveIndexerStatus(cb, &expected, status); err != nil {
		return err
	}
	return attachStatusStream(ctx, cb, expected)
}

// consumeSSEEvents reads SSE events from an HTTP response and dispatches them
// via the provided callbacks. Shared by StreamIndexer and StreamIndexerAttach.
func consumeSSEEvents(ctx context.Context, resp *http.Response, cb IndexerCallbacks, expected *indexerStreamExpectation) error {
	decoder := NewSSEDecoder(ctx, resp.Body)
	for {
		evt, readErr := decoder.Next()
		if errors.Is(readErr, io.EOF) {
			return errIndexerStreamEnded
		}
		if readErr != nil {
			if ctx.Err() != nil {
				return reportIndexerAbort(cb)
			}
			if callbackErr := callOnError(cb, fmt.Sprintf("read error: %v", readErr), 500); callbackErr != nil {
				return fmt.Errorf("on error callback: %w", callbackErr)
			}
			return fmt.Errorf("read SSE: %w", readErr)
		}
		done, err := handleIndexerSSEEvent(cb, evt, expected)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

func handleIndexerSSEEvent(cb IndexerCallbacks, evt SSEEvent, expected *indexerStreamExpectation) (bool, error) {
	switch evt.Type {
	case indexerapi.EventStarted:
		return false, reportIndexerStart(cb, evt.Data, expected)
	case indexerapi.EventProgress, "state":
		return false, reportIndexerProgress(cb, evt.Data, expected)
	case indexerapi.EventComplete:
		return reportIndexerComplete(cb, evt.Data, expected)
	case indexerapi.EventError:
		return false, reportIndexerError(cb, evt.Data, expected)
	default:
		return false, nil
	}
}

func reportIndexerStart(cb IndexerCallbacks, data string, expected *indexerStreamExpectation) error {
	var progress IndexerProgress
	if err := decodeIndexerEvent(data, indexerapi.EventStarted, &progress); err != nil {
		return err
	}
	if err := validateIndexerIdentity(cb, expected, progress.Operation, progress.OperationID, progress.Path); err != nil {
		return err
	}
	if progress.CurrentPath == "" {
		progress.CurrentPath = progress.Path
	}
	normalizeIndexerProgress(&progress)
	if err := callOnProgress(cb, progress); err != nil {
		return fmt.Errorf("on progress callback: %w", err)
	}
	return nil
}

func reportIndexerProgress(cb IndexerCallbacks, data string, expected *indexerStreamExpectation) error {
	var progress IndexerProgress
	if err := decodeIndexerEvent(data, indexerapi.EventProgress, &progress); err != nil {
		return err
	}
	if err := validateIndexerIdentity(cb, expected, progress.Operation, progress.OperationID, progress.Path); err != nil {
		return err
	}
	if progress.CurrentPath == "" {
		progress.CurrentPath = progress.Path
	}
	normalizeIndexerProgress(&progress)
	if err := callOnProgress(cb, progress); err != nil {
		return fmt.Errorf("on progress callback: %w", err)
	}
	return nil
}

func reportIndexerComplete(cb IndexerCallbacks, data string, expected *indexerStreamExpectation) (bool, error) {
	var result IndexerResult
	if err := decodeIndexerEvent(data, indexerapi.EventComplete, &result); err != nil {
		return false, err
	}
	if err := validateIndexerIdentity(cb, expected, result.Operation, result.OperationID, result.Path); err != nil {
		return false, err
	}
	if cb.OnResult != nil {
		if err := cb.OnResult(result); err != nil {
			return false, fmt.Errorf("on result callback: %w", err)
		}
	}
	return true, nil
}

func normalizeIndexerProgress(progress *IndexerProgress) {
	if progress.Phase == "" {
		progress.Phase = progress.State
	}
	switch strings.ToLower(progress.Phase) {
	case "running", "started":
		progress.Phase = "indexing"
	case "scanning":
		progress.Phase = "scan"
	}
	if progress.Phase == "" {
		progress.Phase = "indexing"
	}
	if progress.State == "" {
		progress.State = progress.Phase
	}
}

func validateIndexerOperation(cb IndexerCallbacks, operation string) error {
	if operation == "" || isIndexerOperation(operation) {
		return nil
	}

	message := fmt.Sprintf("cannot attach to %q: it is not an indexing operation", operation)
	if callbackErr := callOnError(cb, message, http.StatusConflict); callbackErr != nil {
		return fmt.Errorf("on error callback: %w", callbackErr)
	}
	return errors.New(message)
}

func validateIndexerIdentity(cb IndexerCallbacks, expected *indexerStreamExpectation, operation, operationID, path string) error {
	if expected == nil {
		return reportIndexerIdentityError(cb, "expected indexer operation identity is missing")
	}
	if expected.operation == "" || expected.operationID == "" || expected.path == "" {
		return reportIndexerIdentityError(cb, "expected indexer operation identity is incomplete")
	}
	if operation == "" || operationID == "" || path == "" {
		return reportIndexerIdentityError(cb, "indexer operation identity is required")
	}
	if operationID != expected.operationID {
		return reportIndexerIdentityError(cb, "indexer operation identity changed")
	}
	if operation != expected.operation {
		return reportIndexerIdentityError(cb, fmt.Sprintf("unexpected indexer operation %q", operation))
	}
	if path != expected.path {
		return reportIndexerIdentityError(cb, fmt.Sprintf("unexpected indexer operation path %q", path))
	}
	return nil
}

func validateActiveIndexerStatus(cb IndexerCallbacks, expected *indexerStreamExpectation, status indexerStatusSnapshot) error {
	if status.Operation == "" {
		return reportIndexerIdentityError(cb, "active indexer status is missing operation")
	}
	if status.OperationID == "" {
		return reportIndexerIdentityError(cb, "active indexer status is missing operation identity")
	}
	statusPath := status.Path
	if statusPath == "" {
		statusPath = "/"
	}
	if err := validateIndexerIdentity(cb, expected, status.Operation, status.OperationID, statusPath); err != nil {
		return err
	}
	return nil
}

func reportIndexerIdentityError(cb IndexerCallbacks, message string) error {
	if callbackErr := callOnError(cb, message, http.StatusConflict); callbackErr != nil {
		return fmt.Errorf("on error callback: %w", callbackErr)
	}
	return errors.New(message)
}

func reportIndexerError(cb IndexerCallbacks, data string, expected *indexerStreamExpectation) error {
	var errData struct {
		Message     string `json:"message"`
		Operation   string `json:"operation"`
		OperationID string `json:"operation_id"`
		Path        string `json:"path"`
	}
	if err := decodeIndexerEvent(data, indexerapi.EventError, &errData); err != nil {
		return err
	}
	if strings.TrimSpace(errData.Message) == "" {
		return errors.New("decode indexer error event: missing message")
	}
	if err := validateIndexerIdentity(cb, expected, errData.Operation, errData.OperationID, errData.Path); err != nil {
		return err
	}
	if callbackErr := callOnError(cb, errData.Message, 500); callbackErr != nil {
		return fmt.Errorf("on error callback: %w", callbackErr)
	}
	return fmt.Errorf("indexer error: %s", errData.Message)
}

func decodeIndexerEvent(data, event string, dst any) error {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" || trimmed == "null" {
		return fmt.Errorf("decode indexer %s event: empty payload", event)
	}
	if err := json.Unmarshal([]byte(trimmed), dst); err != nil {
		return fmt.Errorf("decode indexer %s event: %w", event, err)
	}
	return nil
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
