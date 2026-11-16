package scheduledrun

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
)

// Summary is what a worker says its completed run did, kept as the result of
// the run's record. A run that met failures it raised alerts for, or that did
// no work, still ends as completed: the summary is where the history and the
// end hook read the difference. A run that does not complete stores none.
type Summary struct {
	// Message is one line for the history, such as "1 updated, 1 failed".
	Message string `json:"message"`
	// Failures is how many items (containers, rules, jobs, pools) failed with
	// an alert of their own.
	Failures int `json:"failures,omitzero"`
	// Skipped: the run did no work, since other work held it off. The end
	// hook leaves the unit's run alert as it is.
	Skipped bool `json:"skipped,omitzero"`
}

// ErrNoRun is what Summarize returns for a process that belongs to no
// recorded scheduled run.
var ErrNoRun = errors.New("no scheduled run record")

// ErrCancelRequested is what Summarize returns for a run that was asked to
// stop: it stores nothing, and the run must not end as completed.
var ErrCancelRequested = errors.New("the run was asked to stop")

// Summarize stores the run's summary in its record. A nil reporter returns
// ErrNoRun, and a run that was asked to stop ErrCancelRequested. Workers
// store it through Conclude, which also decides what a summary that was not
// stored means for the exit status.
func (r *Reporter) Summarize(ctx context.Context, summary Summary) error {
	if r == nil {
		return ErrNoRun
	}
	data, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("encode run summary: %w", err)
	}
	if _, err := r.store.Update(ctx, r.id, func(record *jobs.Record) error {
		if record.CancelRequested {
			return ErrCancelRequested
		}
		record.Result = data
		return nil
	}); err != nil {
		if errors.Is(err, ErrCancelRequested) {
			return ErrCancelRequested
		}
		return fmt.Errorf("store run summary: %w", err)
	}
	return nil
}

// summaryTimeout bounds the write of a run's summary.
const summaryTimeout = 5 * time.Second

// Conclude ends a worker's run: it returns the exit status and, for a run
// that completes, stores summary through store (a Reporter's Summarize).
//
// A run whose items failed with alerts of their own, and one that did no
// work, exit with 0, so one container, rule, job or pool does not mark the
// unit and the run as failed; the summary carries the difference. Three runs
// do not complete and exit with 1, storing nothing, so the end hook records
// them as it does any run that did not succeed (failed with the run alert,
// canceled, or interrupted by a shutdown):
//
//   - one with unreported, a failure without an alert of its own (Unreported
//     of the run's error);
//   - one a signal stopped (ctx has ended), whatever its failures were marked;
//   - one that was asked to stop without a signal, as a resync is by a scrub
//     cancel, which Summarize reports.
//
// A skipped run whose summary could not be stored exits with ExitSkipped:
// without it the end hook would read the run as one that ran.
func Conclude(ctx context.Context, store func(context.Context, Summary) error, summary Summary, unreported error) int {
	if unreported != nil || ctx.Err() != nil {
		return 1
	}
	if summary.Message == "" {
		return 0
	}
	storeCtx, cancel := context.WithTimeout(ctx, summaryTimeout)
	err := store(storeCtx, summary)
	cancel()
	switch {
	case err == nil:
		return 0
	case errors.Is(err, ErrCancelRequested), ctx.Err() != nil:
		// Asked to stop, or stopped by a signal during the write.
		return 1
	case !errors.Is(err, ErrNoRun): // a run started by hand has no record
		slog.Warn("failed to store the run summary", "error", err)
	}
	if summary.Skipped {
		return ExitSkipped
	}
	return 0
}

// SummaryOf is the summary a worker stored in record; ok is false when it
// stored none.
func SummaryOf(record jobs.Record) (summary Summary, ok bool) {
	if len(record.Result) == 0 {
		return Summary{}, false
	}
	if err := json.Unmarshal(record.Result, &summary); err != nil || summary.Message == "" {
		return Summary{}, false
	}
	return summary, true
}
