package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// indexWireEvent is the line protocol between the index-mode subprocess and
// the daemon: one JSON object per stdout line, forwarded to SSE clients as
// WorkProgressEvent/WorkCompleteEvent. stdout is reserved for it — index-mode
// logs go to journald or stderr (logging.Configure), never stdout.
type indexWireEvent struct {
	Type    string `json:"type"` // "step", "scan" or "summary"
	Message string `json:"message,omitempty"`

	// scan and summary counters
	Dirs  uint64 `json:"dirs,omitempty"`
	Files uint64 `json:"files,omitempty"`
	Size  uint64 `json:"size,omitempty"`

	// summary only
	DeletedEntries int64  `json:"deleted_entries,omitempty"`
	SkippedDirs    uint64 `json:"skipped_dirs,omitempty"`
	DurationMs     int64  `json:"duration_ms,omitempty"`
}

// WireProgress is the IndexProgress the index-mode subprocess uses: it
// serializes progress as indexWireEvent lines for the parent daemon.
type WireProgress struct {
	enc *json.Encoder
}

func NewWireProgress(w io.Writer) *WireProgress {
	return &WireProgress{enc: json.NewEncoder(w)}
}

func (p *WireProgress) Step(message string) {
	p.emit(indexWireEvent{Type: "step", Message: message})
}

func (p *WireProgress) ScanProgress(dirs, files, size uint64) {
	p.emit(indexWireEvent{Type: "scan", Dirs: dirs, Files: files, Size: size})
}

// Summary reports the final run statistics used by the daemon's SSE event.
func (p *WireProgress) Summary(stats IndexRunStats) {
	p.emit(indexWireEvent{
		Type:           "summary",
		Dirs:           uint64(stats.Dirs),
		Files:          uint64(stats.Files),
		Size:           uint64(stats.TotalSize),
		DeletedEntries: stats.DeletedEntries,
		SkippedDirs:    stats.SkippedDirs,
		DurationMs:     stats.Duration.Milliseconds(),
	})
}

func (p *WireProgress) emit(evt indexWireEvent) {
	if err := p.enc.Encode(evt); err != nil {
		slog.Warn("failed to emit index progress event", "err", err)
	}
}

// runIndexProcess runs a prepared index-mode command, decoding wire events
// from its stdout as they arrive. Each event is handed to onEvent (which may
// be nil); the final summary, when the subprocess emitted one, is also
// returned as run statistics.
func runIndexProcess(cmd *exec.Cmd, onEvent func(indexWireEvent)) (*IndexRunStats, error) {
	cmd.Stderr = os.Stderr
	cancel := func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	if cmd.Cancel != nil {
		cmd.Cancel = cancel
	}
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = 5 * time.Second
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("index subprocess stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("index subprocess start: %w", err)
	}

	summary, decodeErr := decodeIndexProcessOutput(stdout, onEvent)
	var killTimer *time.Timer
	if decodeErr != nil {
		_ = cancel()
		// A manual Cancel does not start exec.Cmd's WaitDelay clock. Force a
		// kill after the same bound so malformed output cannot leave Wait
		// blocked on a child that ignores SIGTERM or remains stuck writing.
		killTimer = time.AfterFunc(cmd.WaitDelay, func() {
			_ = cmd.Process.Kill()
		})
	}
	waitErr := cmd.Wait()
	if killTimer != nil {
		killTimer.Stop()
	}
	if decodeErr != nil {
		if waitErr != nil {
			return nil, fmt.Errorf("%w (wait: %v)", decodeErr, waitErr)
		}
		return nil, decodeErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("index subprocess failed: %w", waitErr)
	}
	return summary, nil
}

func decodeIndexProcessOutput(stdout io.Reader, onEvent func(indexWireEvent)) (*IndexRunStats, error) {
	decoder := json.NewDecoder(stdout)
	var summary *IndexRunStats
	for {
		var evt indexWireEvent
		if err := decoder.Decode(&evt); err != nil {
			if err == io.EOF {
				return summary, nil
			}
			return nil, fmt.Errorf("decode index subprocess output: %w", err)
		}
		if evt.Type == "" {
			return nil, fmt.Errorf("decode index subprocess output: event type is empty")
		}
		if evt.Type == "summary" {
			summary = indexRunStatsFromWireEvent(evt)
		}
		if onEvent != nil {
			onEvent(evt)
		}
	}
}

func indexRunStatsFromWireEvent(evt indexWireEvent) *IndexRunStats {
	return &IndexRunStats{
		Dirs:           int64(evt.Dirs),
		Files:          int64(evt.Files),
		TotalSize:      int64(evt.Size),
		DeletedEntries: evt.DeletedEntries,
		SkippedDirs:    evt.SkippedDirs,
		Duration:       time.Duration(evt.DurationMs) * time.Millisecond,
	}
}
