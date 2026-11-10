package scheduledrun

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
)

// laterNow is when the liveness check runs, an hour after the runs began.
var laterNow = testNow.Add(time.Hour)

// liveness checks h's store as a host of boot that asks active and collects
// its alerts in h.alerts.
func liveness(h *fakeHooks, boot string, active func(ctx context.Context, unit, invocationID string) (bool, error)) Liveness {
	return Liveness{
		Store:  h.Store,
		BootID: boot,
		Active: active,
		Alert:  h.Alert,
		Now:    func() time.Time { return laterNow },
	}
}

func notAsked(t *testing.T) func(context.Context, string, string) (bool, error) {
	return func(_ context.Context, unit, _ string) (bool, error) {
		t.Errorf("systemd asked about %s", unit)
		return false, nil
	}
}

func runningIDs(records []jobs.Record) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	return ids
}

func TestRunningKeepsARunSystemdStillRuns(t *testing.T) {
	h := newHooks(t)
	mustBegin(t, h, testUnit, testInvocation)
	var asked []string
	l := liveness(h, testBoot, func(_ context.Context, unit, invocationID string) (bool, error) {
		asked = append(asked, unit+" "+invocationID)
		return true, nil
	})

	running, err := l.Running(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if ids := runningIDs(running); len(ids) != 1 || ids[0] != testRunID {
		t.Fatalf("running = %v, want [%s]", ids, testRunID)
	}
	if want := testUnit + " " + testInvocation; len(asked) != 1 || asked[0] != want {
		t.Errorf("asked = %v, want [%s]", asked, want)
	}
	if record := mustGet(t, h, testRunID); record.State != jobs.StateRunning {
		t.Errorf("state = %s, want running", record.State)
	}
	if len(h.alerts) != 0 {
		t.Errorf("alerts = %+v, want none", h.alerts)
	}
}

func TestRunningMarksARunFromAnEarlierBootInterrupted(t *testing.T) {
	h := newHooks(t)
	mustBegin(t, h, testUnit, testInvocation)

	running, err := liveness(h, "a-later-boot", notAsked(t)).Running(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(running) != 0 {
		t.Fatalf("running = %v, want none", runningIDs(running))
	}
	record := mustGet(t, h, testRunID)
	if record.State != jobs.StateInterrupted || record.Reason != jobs.ReasonReboot || !record.FinishedAt.Equal(laterNow) {
		t.Errorf("record = %s/%s finished %v, want interrupted/reboot at %v", record.State, record.Reason, record.FinishedAt, laterNow)
	}
	want := alerts.Observation{
		Source:   "jobs",
		Key:      testRunID,
		Severity: alerts.SeverityError,
		Title:    "Nightly backup was interrupted",
		Message:  "The system stopped before the run finished.",
		Link:     testLink,
	}
	if len(h.alerts) != 1 || h.alerts[0] != want {
		t.Errorf("alerts = %+v, want [%+v]", h.alerts, want)
	}
}

func TestRunningMarksAGoneRunCrashed(t *testing.T) {
	h := newHooks(t)
	mustBegin(t, h, testUnit, testInvocation)
	l := liveness(h, testBoot, func(context.Context, string, string) (bool, error) { return false, nil })

	running, err := l.Running(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(running) != 0 {
		t.Fatalf("running = %v, want none", runningIDs(running))
	}
	record := mustGet(t, h, testRunID)
	if record.State != jobs.StateInterrupted || record.Reason != jobs.ReasonCrash || !record.FinishedAt.Equal(laterNow) {
		t.Errorf("record = %s/%s finished %v, want interrupted/crash at %v", record.State, record.Reason, record.FinishedAt, laterNow)
	}
	if len(h.alerts) != 1 || h.alerts[0].Title != "Nightly backup was interrupted" ||
		h.alerts[0].Message != "The run ended without reporting its result." {
		t.Errorf("alerts = %+v, want one crash alert", h.alerts)
	}
}

func TestRunningLeavesARunAloneWhenSystemdCannotBeAsked(t *testing.T) {
	h := newHooks(t)
	mustBegin(t, h, testUnit, testInvocation)
	l := liveness(h, testBoot, func(context.Context, string, string) (bool, error) {
		return false, errors.New("no system bus")
	})

	running, err := l.Running(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if ids := runningIDs(running); len(ids) != 1 || ids[0] != testRunID {
		t.Fatalf("running = %v, want [%s]", ids, testRunID)
	}
	if record := mustGet(t, h, testRunID); record.State != jobs.StateRunning {
		t.Errorf("state = %s, want running", record.State)
	}
	if len(h.alerts) != 0 {
		t.Errorf("alerts = %+v, want none", h.alerts)
	}
}

func TestRunningLeavesARunAloneWhenSystemdDoesNotAnswer(t *testing.T) {
	timeout := activeTimeout
	activeTimeout = 10 * time.Millisecond
	t.Cleanup(func() { activeTimeout = timeout })
	h := newHooks(t)
	mustBegin(t, h, testUnit, testInvocation)
	l := liveness(h, testBoot, func(ctx context.Context, _, _ string) (bool, error) {
		<-ctx.Done() // a wedged systemd
		return false, ctx.Err()
	})
	// Without a bound of its own the check would wait for this context.
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	running, err := l.Running(ctx)
	if err != nil {
		t.Fatalf("Running: %v, want the run left alone after the check timed out", err)
	}
	if ids := runningIDs(running); len(ids) != 1 || ids[0] != testRunID {
		t.Fatalf("running = %v, want [%s]", ids, testRunID)
	}
	if record := mustGet(t, h, testRunID); record.State != jobs.StateRunning {
		t.Errorf("state = %s, want running", record.State)
	}
	if len(h.alerts) != 0 {
		t.Errorf("alerts = %+v, want none", h.alerts)
	}
}

func TestRunningTellsApartTwoInvocationsOfOneUnit(t *testing.T) {
	const (
		earlierInvocation = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		earlierRunID      = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	)
	h := newHooks(t)
	h.BootID = func() (string, error) { return "an-earlier-boot", nil }
	mustBegin(t, h, testUnit, earlierInvocation)
	h.BootID = func() (string, error) { return testBoot, nil }
	mustBegin(t, h, testUnit, testInvocation)
	l := liveness(h, testBoot, func(_ context.Context, _, invocationID string) (bool, error) {
		if invocationID != testInvocation {
			t.Errorf("systemd asked about invocation %s", invocationID)
		}
		return invocationID == testInvocation, nil
	})

	running, err := l.Running(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if ids := runningIDs(running); len(ids) != 1 || ids[0] != testRunID {
		t.Fatalf("running = %v, want [%s]", ids, testRunID)
	}
	if record := mustGet(t, h, earlierRunID); record.State != jobs.StateInterrupted || record.Reason != jobs.ReasonReboot {
		t.Errorf("earlier run = %s/%s, want interrupted/reboot", record.State, record.Reason)
	}
	if record := mustGet(t, h, testRunID); record.State != jobs.StateRunning {
		t.Errorf("current run = %s, want running", record.State)
	}
}

func TestRunningDoesNotAlertForUnitsWithTheirOwnAlerts(t *testing.T) {
	h := newHooks(t)
	mustBegin(t, h, "linuxio-btrfs-snapshot.service", testInvocation)

	if _, err := liveness(h, "a-later-boot", notAsked(t)).Running(t.Context()); err != nil {
		t.Fatal(err)
	}
	if record := mustGet(t, h, testRunID); record.State != jobs.StateInterrupted {
		t.Fatalf("state = %s, want interrupted", record.State)
	}
	if len(h.alerts) != 0 {
		t.Errorf("alerts = %+v, want none", h.alerts)
	}
}
