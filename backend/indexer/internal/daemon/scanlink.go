package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
)

// The scanner's side of a full index (see scan.go for the daemon's).

// scanReportTimeout bounds one report to the daemon.
const scanReportTimeout = 10 * time.Second

// errDaemonGone ends a scan whose connection to the daemon closed: without
// the daemon nothing keeps other writes to the index out of the scan's way.
var errDaemonGone = errors.New("the indexer daemon closed the scan connection")

// NewSocketClient returns a client for the daemon's socket. It has no
// timeout: the scan connection lasts as long as the scan.
func NewSocketClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", version.RunDir+"/indexer.sock")
		},
	}}
}

// ScanLink is the scanner's connection to the daemon. It is the
// IndexProgress of the scan.
type ScanLink struct {
	client      *http.Client
	operationID string
	body        io.Closer
	disconnect  context.CancelFunc
	gone        chan struct{}
}

// OpenScanLink asks the daemon for a full index's turn and returns when it
// has come, which is when the work ahead of it has ended. The wait ends with
// ctx. The link, once open, does not: a scanner that is told to stop still
// has a transaction to roll back, and keeps its turn until it has reported
// how it ended and called Close. The daemon takes the end of the connection
// for the end of a scan that reported no result.
func OpenScanLink(ctx context.Context, client *http.Client) (*ScanLink, error) {
	linkCtx, disconnect := context.WithCancel(context.WithoutCancel(ctx))
	stopWaiting := context.AfterFunc(ctx, disconnect)
	turn, body, err := awaitScanTurn(linkCtx, client)
	if err == nil && !stopWaiting() {
		// Told to stop in the instant the turn came.
		_ = body.Close()
		err = ctx.Err()
	}
	if err != nil {
		stopWaiting()
		disconnect()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("wait for the scan's turn: %w", ctxErr)
		}
		return nil, err
	}

	link := &ScanLink{client: client, operationID: turn, body: body, disconnect: disconnect, gone: make(chan struct{})}
	reader := body
	go func() {
		// Nothing more is sent: the read ends when the connection does.
		_, _ = io.Copy(io.Discard, reader)
		close(link.gone)
	}()
	return link, nil
}

// OperationID names the index this scan is for.
func (l *ScanLink) OperationID() string { return l.operationID }

// Gone is closed when the connection to the daemon has ended.
func (l *ScanLink) Gone() <-chan struct{} { return l.gone }

// Close ends the connection.
func (l *ScanLink) Close() {
	l.disconnect()
	if err := l.body.Close(); err != nil {
		slog.Debug("failed to close the scan connection", "error", err)
	}
}

// awaitScanTurn sends the scan request and reads the line that says the
// scan's turn has come. It returns the operation the scan is for and the
// rest of the response, which stays open for as long as the scan runs.
func awaitScanTurn(ctx context.Context, client *http.Client) (string, io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+api.RouteScan, nil)
	if err != nil {
		return "", nil, fmt.Errorf("create scan request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("reach the indexer daemon: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return "", nil, fmt.Errorf("the indexer daemon refused the scan: %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		_ = resp.Body.Close()
		return "", nil, fmt.Errorf("read the scan's turn: %w", err)
	}
	var turn api.OperationResponse
	if err := json.Unmarshal(line, &turn); err != nil || turn.OperationID == "" {
		_ = resp.Body.Close()
		return "", nil, fmt.Errorf("the indexer daemon named no operation for the scan: %q", strings.TrimSpace(string(line)))
	}
	// Nothing follows the line, so what the reader may hold beyond it is
	// nothing: the body is read on from the connection.
	return turn.OperationID, resp.Body, nil
}

func (l *ScanLink) Step(message string) {
	l.reportProgress(scanEvent{Type: scanEventStep, Message: message})
}

func (l *ScanLink) ScanProgress(dirs, files, size uint64) {
	l.reportProgress(scanEvent{Type: scanEventProgress, Dirs: dirs, Files: files, Size: size})
}

// reportProgress sends progress, which nothing depends on: a report that
// does not arrive is logged and the scan goes on.
func (l *ScanLink) reportProgress(evt scanEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), scanReportTimeout)
	defer cancel()
	if err := l.report(ctx, evt); err != nil {
		slog.Debug("failed to report scan progress", "error", err)
	}
}

// End reports how the scan ended: its totals, or scanErr.
func (l *ScanLink) End(ctx context.Context, stats IndexRunStats, scanErr error) error {
	evt := scanEvent{
		Type:           scanEventSummary,
		Dirs:           uint64(stats.Dirs),
		Files:          uint64(stats.Files),
		Size:           uint64(stats.TotalSize),
		DeletedEntries: stats.DeletedEntries,
		SkippedDirs:    stats.SkippedDirs,
	}
	if scanErr != nil {
		evt = scanEvent{Type: scanEventError, Message: scanErr.Error()}
	}
	return l.report(ctx, evt)
}

func (l *ScanLink) report(ctx context.Context, evt scanEvent) error {
	body, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("encode scan event: %w", err)
	}
	endpoint := "http://unix" + api.RouteScanEvent + "?" + url.Values{"operation_id": {l.operationID}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create scan event request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.client.Do(req)
	if err != nil {
		return fmt.Errorf("send scan event: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("the indexer daemon refused the scan event: %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	return nil
}

// RunScan is the scanner: it takes its turn with the daemon, brings the
// index up to date with the whole filesystem and reports the result. It
// stops when the daemon goes away.
func RunScan(ctx context.Context, cfg configfile.Config, dbPath string, client *http.Client) (IndexRunStats, error) {
	link, err := OpenScanLink(ctx, client)
	if err != nil {
		return IndexRunStats{}, err
	}
	defer link.Close()

	scanCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	go func() {
		select {
		case <-link.Gone():
			cancel(errDaemonGone)
		case <-scanCtx.Done():
		}
	}()

	stats, scanErr := RunIndexMode(scanCtx, cfg, dbPath, link)
	if scanErr != nil && errors.Is(context.Cause(scanCtx), errDaemonGone) {
		return IndexRunStats{}, fmt.Errorf("%w: %w", errDaemonGone, scanErr)
	}
	// Reported under a context of its own: a scan that was told to stop
	// has unwound by now and says so, and only then gives up its turn.
	endCtx, endCancel := context.WithTimeout(context.WithoutCancel(ctx), scanReportTimeout)
	defer endCancel()
	if endErr := link.End(endCtx, stats, scanErr); endErr != nil && scanErr == nil {
		return stats, fmt.Errorf("report the scan's result: %w", endErr)
	}
	return stats, scanErr
}
