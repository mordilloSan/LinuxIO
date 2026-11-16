package scheduledrun

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
)

// A worker stores what its run did, and the record still holds it once the
// end hook has written the outcome.
func TestSummaryIsKeptInTheEndedRecord(t *testing.T) {
	h := newHooks(t)
	mustBegin(t, h, testUnit, testInvocation)
	t.Setenv("INVOCATION_ID", testInvocation)

	want := Summary{Message: "1 updated, 1 failed", Failures: 1}
	if err := ReporterFromEnv(h.Store).Summarize(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	if err := h.End(t.Context(), testUnit, testInvocation, Result{"success", "exited", "0"}); err != nil {
		t.Fatal(err)
	}

	record, err := h.Store.Get(t.Context(), testRunID)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := SummaryOf(record); !ok || got != want || record.State != jobs.StateCompleted {
		t.Fatalf("summary = %+v, %v in a %s record; want %+v in a completed one", got, ok, record.State, want)
	}
}

func TestSummaryOfARecordWithoutOne(t *testing.T) {
	for name, result := range map[string]string{"no result": "", "another shape": `{"updated":true}`, "not an object": `7`} {
		if got, ok := SummaryOf(jobs.Record{Result: []byte(result)}); ok {
			t.Errorf("%s: SummaryOf = %+v, want none", name, got)
		}
	}
	var none *Reporter
	if err := none.Summarize(t.Context(), Summary{Message: "ignored"}); !errors.Is(err, ErrNoRun) {
		t.Errorf("Summarize without a run = %v, want ErrNoRun", err)
	}
}

// A run that did no work completes and says so in its summary: it proves
// nothing about the unit, so a standing run alert stays, where any other
// completed run clears it.
func TestSkippedSummaryChangesNoRunAlert(t *testing.T) {
	for name, tc := range map[string]struct {
		summary  Summary
		resolved int
	}{
		"skipped":           {Summary{Message: "Skipped: another Docker update was running", Skipped: true}, 0},
		"ran with failures": {Summary{Message: "1 failed", Failures: 1}, 1},
	} {
		t.Run(name, func(t *testing.T) {
			const unit = "linuxio-docker-update.service"
			h := newHooks(t)
			resolved := resolving(h)
			mustBegin(t, h, unit, testInvocation)
			t.Setenv("INVOCATION_ID", testInvocation)
			if err := ReporterFromEnv(h.Store).Summarize(t.Context(), tc.summary); err != nil {
				t.Fatal(err)
			}
			if err := h.End(t.Context(), unit, testInvocation, Result{"success", "exited", "0"}); err != nil {
				t.Fatal(err)
			}
			if len(h.alerts) != 0 || len(*resolved) != tc.resolved {
				t.Fatalf("alerts = %+v, resolved = %v; want none raised and %d resolved", h.alerts, *resolved, tc.resolved)
			}
		})
	}
}

// Conclude decides a worker's exit status: only a run that does not complete
// exits with 1, and a skipped run exits with 0 once its summary is stored.
func TestConcludeExitStatus(t *testing.T) {
	unreported := errors.New("docker is not reachable")
	storeErr := errors.New("store is locked")
	skipped := Summary{Message: "Skipped: busy", Skipped: true}
	for name, tc := range map[string]struct {
		summary    Summary
		unreported error
		storeErr   error
		stopped    bool
		stopsLate  bool // a signal arrives during the summary write
		want       int
		stored     bool
	}{
		"ran":                         {summary: Summary{Message: "1 taken"}, want: 0, stored: true},
		"items failed with alerts":    {summary: Summary{Message: "1 failed", Failures: 1}, want: 0, stored: true},
		"no summary":                  {want: 0},
		"skipped":                     {summary: skipped, want: 0, stored: true},
		"skipped, summary not stored": {summary: skipped, storeErr: storeErr, want: ExitSkipped},
		"skipped, no run record":      {summary: skipped, storeErr: ErrNoRun, want: ExitSkipped},
		"ran, summary not stored":     {summary: Summary{Message: "1 taken"}, storeErr: storeErr, want: 0},
		// A run that does not complete stores nothing: its record's state
		// and error say how it ended.
		"failure without an alert": {summary: Summary{Message: "1 failed", Failures: 1}, unreported: unreported, want: 1},
		"no summary, failure":      {unreported: unreported, want: 1},
		// A stopped run must not end as completed, or the end hook could not
		// record it as canceled or interrupted.
		"stopped by a signal":      {summary: Summary{Message: "1 failed", Failures: 1}, stopped: true, want: 1},
		"asked to stop, no signal": {summary: Summary{Message: "Resync failed", Failures: 1}, storeErr: ErrCancelRequested, want: 1},
		"stopped during the write": {summary: Summary{Message: "1 taken"}, storeErr: context.Canceled, stopsLate: true, want: 1},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.stopped {
				cancel()
			}
			stored := false
			store := func(ctx context.Context, _ Summary) error {
				if _, bounded := ctx.Deadline(); !bounded {
					t.Error("the summary write has no deadline")
				}
				if tc.stopsLate {
					cancel()
				}
				stored = tc.storeErr == nil
				return tc.storeErr
			}
			if got := Conclude(ctx, store, tc.summary, tc.unreported); got != tc.want || stored != tc.stored {
				t.Fatalf("Conclude = %d, stored %v; want %d, %v", got, stored, tc.want, tc.stored)
			}
		})
	}
}

// A run that was asked to stop, as a resync is by a scrub cancel, keeps no
// summary: its worker then exits with 1 and the end hook records it canceled.
func TestSummarizeRefusesARunAskedToStop(t *testing.T) {
	h := newHooks(t)
	mustBegin(t, h, testUnit, testInvocation)
	t.Setenv("INVOCATION_ID", testInvocation)
	requestCancel(t, h)

	err := ReporterFromEnv(h.Store).Summarize(t.Context(), Summary{Message: "Resync failed", Failures: 1})
	if !errors.Is(err, ErrCancelRequested) {
		t.Fatalf("Summarize = %v, want ErrCancelRequested", err)
	}
	record, getErr := h.Store.Get(t.Context(), testRunID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if _, ok := SummaryOf(record); ok {
		t.Fatalf("a run asked to stop kept the summary %s", record.Result)
	}
}

func TestUnreported(t *testing.T) {
	reported := Reported(errors.New("web: pull failed"))
	other := errors.New("a scheduled container is gone")
	if got := Unreported(nil); got != nil {
		t.Errorf("Unreported(nil) = %v", got)
	}
	if got := Unreported(reported); got != nil {
		t.Errorf("a reported failure left %v", got)
	}
	if got := Unreported(errors.Join(reported, fmt.Errorf("db: %w", reported))); got != nil {
		t.Errorf("reported failures left %v", got)
	}
	if got := Unreported(other); !errors.Is(got, other) {
		t.Errorf("Unreported(other) = %v", got)
	}
	got := Unreported(errors.Join(reported, other))
	if !errors.Is(got, other) || AllReported(got) || got.Error() != other.Error() {
		t.Errorf("Unreported(joined) = %v, want only %v", got, other)
	}
	// A pass joins the joined failures of its steps.
	got = Unreported(errors.Join(nil, errors.Join(reported, other, reported)))
	if got == nil || got.Error() != other.Error() {
		t.Errorf("Unreported(nested) = %v, want only %v", got, other)
	}
}
