package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
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

// parseIndexWireLine decodes one subprocess stdout line. Lines that are not
// wire events (e.g. stray tool output) report ok=false and are passed through.
func parseIndexWireLine(line []byte) (evt indexWireEvent, ok bool) {
	if len(line) == 0 || line[0] != '{' {
		return indexWireEvent{}, false
	}
	if err := json.Unmarshal(line, &evt); err != nil || evt.Type == "" {
		return indexWireEvent{}, false
	}
	return evt, true
}

// runIndexProcess runs a prepared index-mode command, decoding wire events
// from its stdout as they arrive. Each event is handed to onEvent (which may
// be nil); the final summary, when the subprocess emitted one, is also
// returned as run statistics.
func runIndexProcess(cmd *exec.Cmd, onEvent func(indexWireEvent)) (*IndexRunStats, error) {
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("index subprocess stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("index subprocess start: %w", err)
	}

	var summary *IndexRunStats
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		evt, ok := parseIndexWireLine(scanner.Bytes())
		if !ok {
			fmt.Fprintf(os.Stdout, "%s\n", scanner.Bytes())
			continue
		}
		if evt.Type == "summary" {
			summary = &IndexRunStats{
				Dirs:           int64(evt.Dirs),
				Files:          int64(evt.Files),
				TotalSize:      int64(evt.Size),
				DeletedEntries: evt.DeletedEntries,
				SkippedDirs:    evt.SkippedDirs,
				Duration:       time.Duration(evt.DurationMs) * time.Millisecond,
			}
		}
		if onEvent != nil {
			onEvent(evt)
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		slog.Warn("reading index subprocess output failed", "err", scanErr)
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("index subprocess failed: %w", err)
	}
	return summary, nil
}
