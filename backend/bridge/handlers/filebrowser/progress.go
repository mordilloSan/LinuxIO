package filebrowser

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

const (
	// progressReportIntervalBytes is the copy buffer size for streamed files.
	progressReportIntervalBytes = 2 * 1024 * 1024
	// uploadProgressAckIntervalBytes is more frequent because upload progress acts as a client-side flow-control ACK.
	uploadProgressAckIntervalBytes = 512 * 1024
	// transferProgressMaxInterval keeps visible transfer feedback cadence-based instead of purely byte-based.
	transferProgressMaxInterval = 250 * time.Millisecond
	// transferProgressMaxBytes caps silence on very fast transfers without forcing fixed tiny byte steps.
	transferProgressMaxBytes = 32 * 1024 * 1024
)

// progressThrottle is the one report-rate policy for filebrowser progress. A
// report goes out only when the work advanced, and then when it is the first
// report, the last (processed reached total), or one of:
//   - paced: at least minInterval elapsed and either minBytes advanced or the
//     whole percentage did. Job progress bars use this.
//   - keep-alive: maxInterval elapsed or maxBytes advanced. Stream transfers use
//     this because their reports double as the client's flow-control ACK.
//
// A zero minInterval and minBytes disable pacing; a zero max disables that
// keep-alive limit. It is safe for the concurrent callbacks of worker pools.
type progressThrottle struct {
	mu          sync.Mutex
	minInterval time.Duration
	minBytes    int64
	maxInterval time.Duration
	maxBytes    int64

	processed     int64
	total         int64
	lastProcessed int64
	lastPct       int
	lastAt        time.Time
}

// newTransferGate throttles stream transfers on cadence and a byte cap.
func newTransferGate(maxBytes int64) *progressThrottle {
	if maxBytes <= 0 {
		maxBytes = transferProgressMaxBytes
	}
	return &progressThrottle{maxInterval: transferProgressMaxInterval, maxBytes: maxBytes, lastPct: -1}
}

// newJobThrottle paces byte progress with the user's job settings.
func newJobThrottle(settings userconfig.PersistedJobSettings) *progressThrottle {
	settings = userconfig.EffectiveJobSettings(settings)
	return &progressThrottle{
		minInterval: time.Duration(settings.ProgressMinIntervalMs) * time.Millisecond,
		minBytes:    int64(settings.ProgressMinBytesMB) * 1024 * 1024,
		lastPct:     -1,
	}
}

// newCountThrottle paces item-count progress: any advance counts.
func newCountThrottle(settings userconfig.PersistedJobSettings) *progressThrottle {
	settings = userconfig.EffectiveJobSettings(settings)
	return &progressThrottle{
		minInterval: time.Duration(settings.ProgressMinIntervalMs) * time.Millisecond,
		minBytes:    1,
		lastPct:     -1,
	}
}

// Add accumulates n and reports whether the new position should be sent.
func (g *progressThrottle) Add(n int64) (int64, int, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if n > 0 {
		g.processed += n
	}
	return g.check(time.Now())
}

// Set records an absolute position and reports whether it should be sent.
func (g *progressThrottle) Set(processed, total int64) (int64, int, bool) {
	return g.setAt(processed, total, time.Now())
}

func (g *progressThrottle) setAt(processed, total int64, now time.Time) (int64, int, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.processed, g.total = processed, total
	return g.check(now)
}

// SetTotal installs the denominator once a late-arriving size estimate lands.
// Until then only byte pacing applies, since the percentage needs a total.
func (g *progressThrottle) SetTotal(total int64) {
	g.mu.Lock()
	g.total = total
	g.mu.Unlock()
}

func (g *progressThrottle) check(now time.Time) (int64, int, bool) {
	pct := transferPct(g.processed, g.total)
	delta := g.processed - g.lastProcessed
	if delta <= 0 {
		return g.processed, pct, false
	}
	silence := now.Sub(g.lastAt)
	paced := g.minInterval > 0 || g.minBytes > 0
	switch {
	case g.total > 0 && g.processed >= g.total, g.lastAt.IsZero():
	case paced && silence >= g.minInterval && (delta >= g.minBytes || pct > g.lastPct):
	case g.maxBytes > 0 && delta >= g.maxBytes, g.maxInterval > 0 && silence >= g.maxInterval:
	default:
		return g.processed, pct, false
	}
	g.lastProcessed, g.lastPct, g.lastAt = g.processed, pct, now
	return g.processed, pct, true
}

// newProgressCallbacks adapts the byte callbacks of the archive and file
// services into throttled reports. The size is re-read on every call because
// archive estimates land mid-operation. A total known to be zero means there
// is nothing to report against; a merely unknown one still reports, flagged
// indeterminate so the client shows a busy bar.
func newProgressCallbacks(ctx context.Context, throttle *progressThrottle, size *sizeEstimate, emit func(processed, total int64, pct int, indeterminate bool)) *services.OperationCallbacks {
	return &services.OperationCallbacks{
		Cancel: func() bool { return ctx.Err() != nil },
		Progress: func(n int64) {
			total, known := size.get()
			if known && total <= 0 {
				return
			}
			throttle.SetTotal(total)
			if processed, pct, ok := throttle.Add(n); ok {
				emit(processed, total, pct, !known)
			}
		},
	}
}

// newTaskPhaseCallbacks reports a Task's byte progress under one phase name.
func newTaskPhaseCallbacks(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, size *sizeEstimate, phase string) *services.OperationCallbacks {
	throttle := newJobThrottle(taskSettingsForTask(ctx, task, store))
	return newProgressCallbacks(ctx, throttle, size, func(processed, total int64, pct int, indeterminate bool) {
		task.ReportProgress(FileProgress{Bytes: processed, Total: total, Pct: pct, Phase: phase, Indeterminate: indeterminate})
	})
}

// writeTaskPhaseProgress announces a phase before any bytes have moved. A
// zero total means the denominator is still being computed, which the client
// renders as a busy bar rather than a stalled 0%.
func writeTaskPhaseProgress(task *bridgeipc.Task, total int64, phase string) {
	task.ReportProgress(FileProgress{
		Total:         total,
		Phase:         phase,
		Indeterminate: total <= 0,
	})
}

func taskSettingsForTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore) userconfig.PersistedJobSettings {
	if task == nil || strings.TrimSpace(task.Owner().Username) == "" {
		return userconfig.DefaultJobSettings()
	}
	cfg, _, err := userconfig.SnapshotForUser(ctx, task.Owner().Username, store)
	if err != nil || cfg == nil {
		return userconfig.DefaultJobSettings()
	}
	return userconfig.EffectiveJobSettings(cfg.Jobs)
}

func transferPct(bytes, total int64) int {
	if total <= 0 {
		return 0
	}
	return min(int(bytes*100/total), 100)
}

// FileProgress represents progress for file transfer and file task operations.
type FileProgress struct {
	Bytes int64  `json:"bytes"`           // Bytes transferred so far
	Total int64  `json:"total"`           // Total bytes (0 if unknown)
	Pct   int    `json:"pct"`             // Percentage (0-100)
	Phase string `json:"phase,omitempty"` // Optional phase description
	// Indeterminate marks progress whose total is not known yet, so Pct is
	// meaningless and the client should show a busy bar rather than 0%. Archive
	// compression starts before its size estimate finishes and clears this once
	// the estimate lands.
	Indeterminate bool `json:"indeterminate,omitempty"`
}

func (p FileProgress) ProgressEnvelope() bridgeipc.TaskProgress {
	return bridgeipc.TaskProgress{Percentage: &p.Pct, Phase: p.Phase, Detail: p}
}

// BatchUploadProgress reports aggregate byte progress for a batch upload plus
// how many manifest files have been fully processed. Bytes doubles as the
// client's flow-control ack watermark, so it counts stream bytes consumed
// (including discarded bytes of failed items), not bytes durably written.
type BatchUploadProgress struct {
	Bytes      int64  `json:"bytes"`
	Total      int64  `json:"total"`
	Pct        int    `json:"pct"`
	Phase      string `json:"phase,omitempty"`
	FilesDone  int    `json:"filesDone"`
	FilesTotal int    `json:"filesTotal"`
}

func (p BatchUploadProgress) ProgressEnvelope() bridgeipc.TaskProgress {
	return bridgeipc.TaskProgress{Percentage: &p.Pct, Phase: p.Phase, Detail: p}
}

// CountProgress represents item-count progress for batch tasks. When the
// entry total is unknown, Indeterminate is set and the envelope omits the
// percentage so clients show a running count instead of a frozen 0%.
type CountProgress struct {
	Processed     int64  `json:"processed"`
	Total         int64  `json:"total"`
	Pct           int    `json:"pct"`
	Phase         string `json:"phase,omitempty"`
	Indeterminate bool   `json:"indeterminate,omitempty"`
}

func (p CountProgress) ProgressEnvelope() bridgeipc.TaskProgress {
	envelope := bridgeipc.TaskProgress{Phase: p.Phase, Detail: p}
	if !p.Indeterminate {
		envelope.Percentage = &p.Pct
	}
	return envelope
}
