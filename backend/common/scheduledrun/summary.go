package scheduledrun

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
)

// Summary is what a worker says its run did, kept as the result of the run's
// record. A run that met failures it raised alerts for, or that did no work,
// still ends as completed: the summary is where the history and the end hook
// read the difference.
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

// Summarize stores the run's summary in its record. A nil reporter returns
// ErrNoRun. The caller decides what a summary that was not stored means: a
// worker whose run did no work must then exit with ExitSkipped, since the
// end hook would read its completed run as one that ran.
func (r *Reporter) Summarize(ctx context.Context, summary Summary) error {
	if r == nil {
		return ErrNoRun
	}
	data, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("encode run summary: %w", err)
	}
	if _, err := r.store.Update(ctx, r.id, func(record *jobs.Record) error {
		record.Result = data
		return nil
	}); err != nil {
		return fmt.Errorf("store run summary: %w", err)
	}
	return nil
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
