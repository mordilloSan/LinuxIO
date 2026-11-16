package scheduledrun

import (
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
