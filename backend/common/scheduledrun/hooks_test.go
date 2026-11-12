package scheduledrun

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
)

const (
	testBoot       = "11111111-2222-3333-4444-555555555555"
	testInvocation = "1234567890abcdef1234567890abcdef"
)

var (
	testNow    = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	testUnit   = "linuxio-schedule-" + testScheduleID + ".service"
	testRunID  = "12345678-90ab-cdef-1234-567890abcdef"
	testLink   = "/services/schedules?schedule=" + testScheduleID
	testScript = definitionNamed(testScheduleID, "Nightly backup")
)

// fakeHooks is Hooks over a temp store with a fixed clock; alerts collects the
// alerts raised and stopping is what the host reports.
type fakeHooks struct {
	Hooks
	alerts   []alerts.Observation
	stopping bool
}

func newHooks(t *testing.T) *fakeHooks {
	t.Helper()
	dir := t.TempDir()
	scheduleDir := filepath.Join(dir, "schedules")
	writeDefinition(t, scheduleDir, testScheduleID, testScript)
	f := &fakeHooks{}
	f.Hooks = Hooks{
		Store:       jobs.NewStore(filepath.Join(dir, "state"), filepath.Join(dir, "run")),
		ScheduleDir: scheduleDir,
		Now:         func() time.Time { return testNow },
		BootID:      func() (string, error) { return testBoot, nil },
		Output:      func(context.Context, string, io.Writer) error { return nil },
		Stopping:    func(context.Context) bool { return f.stopping },
		Alert: func(_ context.Context, obs alerts.Observation) error {
			f.alerts = append(f.alerts, obs)
			return nil
		},
	}
	return f
}

func mustGet(t *testing.T, h *fakeHooks, id string) jobs.Record {
	t.Helper()
	record, err := h.Store.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("Get %s: %v", id, err)
	}
	return record
}

func mustBegin(t *testing.T, h *fakeHooks, unit, invocation string) {
	t.Helper()
	if err := h.Begin(t.Context(), unit, invocation); err != nil {
		t.Fatalf("Begin: %v", err)
	}
}

func requestCancel(t *testing.T, h *fakeHooks) {
	t.Helper()
	if _, err := h.Store.Update(t.Context(), testRunID, func(r *jobs.Record) error {
		r.CancelRequested = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBeginCreatesTheRunningRecord(t *testing.T) {
	h := newHooks(t)
	mustBegin(t, h, testUnit, testInvocation)
	record := mustGet(t, h, testRunID)

	if record.Route != RouteScript || !slices.Equal(record.Target, []string{testUnit}) || record.State != jobs.StateRunning {
		t.Errorf("route, target, state = %q, %v, %q", record.Route, record.Target, record.State)
	}
	if !record.CreatedAt.Equal(testNow) || !record.StartedAt.Equal(testNow) {
		t.Errorf("created %v, started %v; want %v", record.CreatedAt, record.StartedAt, testNow)
	}
	if record.Owner != (jobs.Owner{UID: 0, Username: "root"}) {
		t.Errorf("owner = %+v", record.Owner)
	}
	wantExecutor := jobs.Executor{Kind: "unit", Unit: testUnit, InvocationID: testInvocation, BootID: testBoot}
	if record.Executor != wantExecutor {
		t.Errorf("executor = %+v, want %+v", record.Executor, wantExecutor)
	}
	if record.Protocol != jobs.Protocol {
		t.Errorf("protocol = %d", record.Protocol)
	}
	var got, want any
	if err := json.Unmarshal(record.Metadata, &got); err != nil {
		t.Fatalf("metadata %s: %v", record.Metadata, err)
	}
	if err := json.Unmarshal([]byte(`{"label":"Nightly backup","identity":["`+testScheduleID+`"]}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("metadata = %s", record.Metadata)
	}
}

func TestBeginOmitsTheIdentityOfASingleUnit(t *testing.T) {
	h := newHooks(t)
	mustBegin(t, h, "linuxio-docker-update.service", testInvocation)
	record := mustGet(t, h, testRunID)
	var got map[string]any
	if err := json.Unmarshal(record.Metadata, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, map[string]any{"label": "Container updates"}) {
		t.Errorf("metadata = %s", record.Metadata)
	}
}

func TestBeginTwiceKeepsTheFirstRecord(t *testing.T) {
	h := newHooks(t)
	mustBegin(t, h, testUnit, testInvocation)
	first := mustGet(t, h, testRunID)
	h.Now = func() time.Time { return testNow.Add(time.Hour) }
	if err := h.Begin(t.Context(), testUnit, testInvocation); err != nil {
		t.Fatalf("second Begin: %v", err)
	}
	if got := mustGet(t, h, testRunID); !reflect.DeepEqual(got, first) {
		t.Errorf("record changed:\n%+v\n%+v", got, first)
	}
}

func TestBeginRefusesAnUnknownUnit(t *testing.T) {
	h := newHooks(t)
	if err := h.Begin(t.Context(), "ssh.service", testInvocation); err == nil {
		t.Fatal("Begin accepted ssh.service")
	}
	records, err := h.Store.List(t.Context())
	if err != nil || len(records) != 0 {
		t.Fatalf("List = %v, %v; want no records", records, err)
	}
}

func TestEndOutcomes(t *testing.T) {
	aborted := &jobs.Error{Code: 499, Message: "operation aborted"}
	failed := func(message string) *jobs.Error { return &jobs.Error{Code: 500, Message: message} }
	tests := []struct {
		name       string
		result     Result
		cancel     bool
		stopping   bool
		wantState  jobs.State
		wantReason jobs.Reason
		wantError  *jobs.Error
	}{
		{"success", Result{"success", "exited", "0"}, false, false, jobs.StateCompleted, "", nil},
		{"success after cancel", Result{"success", "exited", "0"}, true, false, jobs.StateCompleted, "", nil},
		{"exit code", Result{"exit-code", "exited", "3"}, false, false, jobs.StateFailed, "", failed("exited with status 3")},
		{"signal", Result{"signal", "killed", "KILL"}, false, false, jobs.StateFailed, "", failed("killed by signal KILL")},
		{"core dump", Result{"core-dump", "dumped", "SEGV"}, false, false, jobs.StateFailed, "", failed("killed by signal SEGV")},
		{"timeout", Result{"timeout", "killed", "TERM"}, false, false, jobs.StateFailed, "", failed("timed out")},
		{"oom", Result{"oom-kill", "killed", "KILL"}, false, false, jobs.StateFailed, "", failed("killed for using too much memory")},
		{"start limit", Result{"start-limit-hit", "", ""}, false, false, jobs.StateFailed, "", failed("failed: start-limit-hit")},
		{"canceled", Result{"signal", "killed", "TERM"}, true, false, jobs.StateCanceled, "", aborted},
		{"canceled while stopping", Result{"signal", "killed", "TERM"}, true, true, jobs.StateCanceled, "", aborted},
		{"interrupted", Result{"signal", "killed", "TERM"}, false, true, jobs.StateInterrupted, jobs.ReasonShutdown, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHooks(t)
			h.stopping = tt.stopping
			mustBegin(t, h, testUnit, testInvocation)
			if tt.cancel {
				requestCancel(t, h)
			}
			h.Now = func() time.Time { return testNow.Add(time.Minute) }
			if err := h.End(t.Context(), testUnit, testInvocation, tt.result); err != nil {
				t.Fatalf("End: %v", err)
			}
			record := mustGet(t, h, testRunID)
			if record.State != tt.wantState || record.Reason != tt.wantReason {
				t.Errorf("state, reason = %q, %q; want %q, %q", record.State, record.Reason, tt.wantState, tt.wantReason)
			}
			if !reflect.DeepEqual(record.Error, tt.wantError) {
				t.Errorf("error = %+v, want %+v", record.Error, tt.wantError)
			}
			if want := testNow.Add(time.Minute); !record.FinishedAt.Equal(want) {
				t.Errorf("finished at %v, want %v", record.FinishedAt, want)
			}
		})
	}
}

func TestEndWithoutBeginStillRecordsTheRun(t *testing.T) {
	h := newHooks(t)
	if _, err := h.Store.Get(t.Context(), testRunID); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("Get before End = %v, want ErrNotFound", err)
	}
	if err := h.End(t.Context(), testUnit, testInvocation, Result{"exit-code", "exited", "1"}); err != nil {
		t.Fatalf("End: %v", err)
	}
	if records, err := h.Store.List(t.Context()); err != nil || len(records) != 1 {
		t.Fatalf("List = %d records, %v; want exactly 1", len(records), err)
	}
	record := mustGet(t, h, testRunID)
	if record.State != jobs.StateFailed || !record.CreatedAt.Equal(testNow) || !record.FinishedAt.Equal(testNow) {
		t.Errorf("record = state %q, created %v, finished %v", record.State, record.CreatedAt, record.FinishedAt)
	}
	if record.Route != RouteScript || record.Executor.InvocationID != testInvocation {
		t.Errorf("route %q, executor %+v", record.Route, record.Executor)
	}
	if len(h.alerts) != 1 {
		t.Errorf("alerts = %d, want 1", len(h.alerts))
	}
}

func TestEndKeepsARunShorterThanASecond(t *testing.T) {
	h := newHooks(t)
	mustBegin(t, h, testUnit, testInvocation)
	if err := h.End(t.Context(), testUnit, testInvocation, Result{"success", "exited", "0"}); err != nil {
		t.Fatalf("End: %v", err)
	}
	if record := mustGet(t, h, testRunID); record.State != jobs.StateCompleted {
		t.Errorf("state = %q", record.State)
	}
}

func TestEndIsHarmlessOnATerminalRecord(t *testing.T) {
	h := newHooks(t)
	mustBegin(t, h, testUnit, testInvocation)
	if err := h.End(t.Context(), testUnit, testInvocation, Result{"exit-code", "exited", "3"}); err != nil {
		t.Fatal(err)
	}
	first := mustGet(t, h, testRunID)
	h.Now = func() time.Time { return testNow.Add(time.Hour) }
	if err := h.End(t.Context(), testUnit, testInvocation, Result{"success", "exited", "0"}); err != nil {
		t.Fatalf("second End: %v", err)
	}
	if got := mustGet(t, h, testRunID); !reflect.DeepEqual(got, first) {
		t.Errorf("record changed:\n%+v\n%+v", got, first)
	}
	if len(h.alerts) != 1 {
		t.Errorf("alerts = %d, want 1", len(h.alerts))
	}
}

func TestEndAlertsOnlyForScripts(t *testing.T) {
	failed := Result{"exit-code", "exited", "3"}
	killed := Result{"signal", "killed", "TERM"}

	h := newHooks(t)
	mustBegin(t, h, testUnit, testInvocation)
	if err := h.End(t.Context(), testUnit, testInvocation, failed); err != nil {
		t.Fatal(err)
	}
	want := alerts.Observation{
		Source:   "jobs",
		Key:      testRunID,
		Severity: alerts.SeverityError,
		Title:    "Nightly backup failed",
		Message:  "exited with status 3",
		Link:     testLink,
	}
	if len(h.alerts) != 1 || h.alerts[0] != want {
		t.Errorf("failed script alerts = %+v, want [%+v]", h.alerts, want)
	}

	h = newHooks(t)
	h.stopping = true
	mustBegin(t, h, testUnit, testInvocation)
	if err := h.End(t.Context(), testUnit, testInvocation, killed); err != nil {
		t.Fatal(err)
	}
	want.Severity = alerts.SeverityWarning
	want.Title = "Nightly backup was interrupted"
	want.Message = "The system stopped before the run finished."
	if len(h.alerts) != 1 || h.alerts[0] != want {
		t.Errorf("interrupted script alerts = %+v, want [%+v]", h.alerts, want)
	}

	quiet := []struct {
		name   string
		unit   string
		result Result
		cancel bool
	}{
		{"completed script", testUnit, Result{"success", "exited", "0"}, false},
		{"canceled script", testUnit, killed, true},
	}
	for _, tt := range quiet {
		h = newHooks(t)
		mustBegin(t, h, tt.unit, testInvocation)
		if tt.cancel {
			requestCancel(t, h)
		}
		if err := h.End(t.Context(), tt.unit, testInvocation, tt.result); err != nil {
			t.Fatal(err)
		}
		if len(h.alerts) != 0 {
			t.Errorf("%s raised %+v", tt.name, h.alerts)
		}
	}
}

// reportingScripts is the settings in which scheduled scripts report report,
// and the schedules in scripts what they name.
func reportingScripts(report string, scripts map[string]string) func() alerts.NotificationSettings {
	return func() alerts.NotificationSettings {
		n := alerts.DefaultNotifications()
		n.Report[alerts.KindScripts] = report
		n.Scripts = scripts
		return n
	}
}

// completeRun begins and ends a successful run of testUnit with invocation,
// which takes ran.
func completeRun(t *testing.T, h *fakeHooks, invocation string, ran time.Duration) {
	t.Helper()
	h.Now = func() time.Time { return testNow }
	mustBegin(t, h, testUnit, invocation)
	h.Now = func() time.Time { return testNow.Add(ran) }
	if err := h.End(t.Context(), testUnit, invocation, Result{"success", "exited", "0"}); err != nil {
		t.Fatalf("End: %v", err)
	}
}

func TestEndReportsEveryRunWhenAsked(t *testing.T) {
	h := newHooks(t)
	h.Settings = reportingScripts(alerts.ReportEveryRun, nil)
	completeRun(t, h, testInvocation, 12*time.Minute)
	want := alerts.Observation{
		Source:       "jobs",
		Key:          "finished-" + testScheduleID,
		Severity:     alerts.SeverityInfo,
		Title:        "Nightly backup finished",
		Message:      "It ran for 12 min.",
		Link:         testLink,
		OccurrenceID: testRunID,
	}
	if len(h.alerts) != 1 || h.alerts[0] != want {
		t.Errorf("alerts = %+v, want [%+v]", h.alerts, want)
	}

	h = newHooks(t)
	h.Settings = reportingScripts(alerts.ReportFailures, nil)
	completeRun(t, h, testInvocation, 12*time.Minute)
	if len(h.alerts) != 0 {
		t.Errorf("with failures only a completed script raised %+v", h.alerts)
	}

	// Each run is a new occurrence of the schedule's one notice.
	path := alerts.Path
	alerts.Path = filepath.Join(t.TempDir(), "alerts.json")
	t.Cleanup(func() { alerts.Path = path })
	const (
		secondInvocation = "fedcba0987654321fedcba0987654321"
		secondRunID      = "fedcba09-8765-4321-fedc-ba0987654321"
	)
	h = newHooks(t)
	h.Settings = reportingScripts(alerts.ReportEveryRun, nil)
	h.Alert = alerts.Raise
	completeRun(t, h, testInvocation, time.Minute)
	if err := alerts.MarkSeen(t.Context(), "1000", nil); err != nil {
		t.Fatal(err)
	}
	doc, err := alerts.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Alerts) != 1 || doc.Alerts[0].Seen["1000"] != 1 {
		t.Fatalf("after the first run alerts = %+v, want one notice seen once", doc.Alerts)
	}
	completeRun(t, h, secondInvocation, time.Minute)
	if doc, err = alerts.Load(); err != nil {
		t.Fatal(err)
	}
	if len(doc.Alerts) != 1 {
		t.Fatalf("alerts = %+v, want the schedule's one notice", doc.Alerts)
	}
	a := doc.Alerts[0]
	if a.ID != "jobs/finished-"+testScheduleID || a.OccurrenceID != secondRunID || a.OccurrenceCount != 2 {
		t.Errorf("notice = %s, occurrence %s of %d; want jobs/finished-%s, occurrence %s of 2", a.ID, a.OccurrenceID, a.OccurrenceCount, testScheduleID, secondRunID)
	}
	if a.Seen["1000"] >= a.OccurrenceCount {
		t.Errorf("seen %d of %d occurrences; want the newer run unseen", a.Seen["1000"], a.OccurrenceCount)
	}
}

func TestEndFollowsTheScriptsOverride(t *testing.T) {
	for _, tc := range []struct {
		name, report, override string
		notices                int
	}{
		{"override asks for every run", alerts.ReportFailures, alerts.ReportEveryRun, 1},
		{"override asks for failures", alerts.ReportEveryRun, alerts.ReportFailures, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHooks(t)
			h.Settings = reportingScripts(tc.report, map[string]string{testScheduleID: tc.override})
			completeRun(t, h, testInvocation, time.Minute)
			if len(h.alerts) != tc.notices {
				t.Fatalf("alerts = %+v, want %d notices", h.alerts, tc.notices)
			}
			if tc.notices == 1 && h.alerts[0].Title != "Nightly backup finished" {
				t.Errorf("notice = %+v, want one titled %q", h.alerts[0], "Nightly backup finished")
			}
		})
	}
}

// A run cut short by a shutdown or a reboot is a warning; one whose end hook
// never reported (a crash) is a failure in effect, an alert.
func TestInterruptedScriptLevelFollowsItsReason(t *testing.T) {
	want := alerts.Observation{
		Source:   "jobs",
		Key:      testRunID,
		Severity: alerts.SeverityWarning,
		Title:    "Nightly backup was interrupted",
		Message:  "The system stopped before the run finished.",
		Link:     testLink,
	}
	t.Run("by a shutdown, in End", func(t *testing.T) {
		h := newHooks(t)
		h.stopping = true
		mustBegin(t, h, testUnit, testInvocation)
		if err := h.End(t.Context(), testUnit, testInvocation, Result{"signal", "killed", "TERM"}); err != nil {
			t.Fatal(err)
		}
		if len(h.alerts) != 1 || h.alerts[0] != want {
			t.Errorf("alerts = %+v, want [%+v]", h.alerts, want)
		}
	})
	t.Run("by a reboot, in Liveness", func(t *testing.T) {
		h := newHooks(t)
		mustBegin(t, h, testUnit, testInvocation)
		if _, err := liveness(h, "a-later-boot", notAsked(t)).Running(t.Context()); err != nil {
			t.Fatal(err)
		}
		if len(h.alerts) != 1 || h.alerts[0] != want {
			t.Errorf("alerts = %+v, want [%+v]", h.alerts, want)
		}
	})
	t.Run("by a crash, in Liveness", func(t *testing.T) {
		h := newHooks(t)
		mustBegin(t, h, testUnit, testInvocation)
		gone := func(context.Context, string, string) (bool, error) { return false, nil }
		if _, err := liveness(h, testBoot, gone).Running(t.Context()); err != nil {
			t.Fatal(err)
		}
		crashed := want
		crashed.Severity = alerts.SeverityError
		crashed.Message = "The run ended without reporting its result."
		if len(h.alerts) != 1 || h.alerts[0] != crashed {
			t.Errorf("alerts = %+v, want [%+v]", h.alerts, crashed)
		}
	})
}

func TestCompletedSnapshotUnitRaisesNothingHere(t *testing.T) {
	everyRun := func() alerts.NotificationSettings {
		n := alerts.DefaultNotifications()
		for _, kind := range []string{alerts.KindScripts, alerts.KindSnapshots, alerts.KindDockerUpdates} {
			n.Report[kind] = alerts.ReportEveryRun
		}
		n.Report[alerts.KindResync] = alerts.ReportChanges
		return n
	}
	// Their own sources report these runs.
	for _, unit := range []string{
		"linuxio-btrfs-snapshot.service",
		"linuxio-docker-update.service",
		"linuxio-btrfs-resync@" + testScheduleID + ".service",
	} {
		h := newHooks(t)
		h.Settings = everyRun
		mustBegin(t, h, unit, testInvocation)
		if err := h.End(t.Context(), unit, testInvocation, Result{"success", "exited", "0"}); err != nil {
			t.Fatalf("End %s: %v", unit, err)
		}
		if len(h.alerts) != 0 {
			t.Errorf("a completed run of %s raised %+v", unit, h.alerts)
		}
	}
}

func TestDefaultHooksReadTheNotificationSettings(t *testing.T) {
	path := alerts.Path
	alerts.Path = filepath.Join(t.TempDir(), "alerts.json")
	t.Cleanup(func() { alerts.Path = path })
	settings := `{"report":{"scheduled_scripts":"every_run"}}`
	if err := os.WriteFile(filepath.Join(filepath.Dir(alerts.Path), "notifications.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	hooks := DefaultHooks()
	if got := hooks.Settings().ReportFor(alerts.KindScripts); got != alerts.ReportEveryRun {
		t.Errorf("scripts report = %q, want the stored every_run", got)
	}
	// A worker run with nothing left to report clears its run alert.
	if hooks.Resolve == nil {
		t.Error("Resolve is not wired")
	}
}

func TestEndPrunesTheUnitsOlderRuns(t *testing.T) {
	h := newHooks(t)
	oldest := seedEndedRuns(t, h)
	mustBegin(t, h, testUnit, testInvocation)
	if err := h.End(t.Context(), testUnit, testInvocation, Result{"success", "exited", "0"}); err != nil {
		t.Fatalf("End: %v", err)
	}
	records, err := h.Store.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != jobs.MaxScheduledRunsPerUnit {
		t.Errorf("%d records, want %d", len(records), jobs.MaxScheduledRunsPerUnit)
	}
	if _, err := h.Store.Get(t.Context(), oldest); !errors.Is(err, jobs.ErrNotFound) {
		t.Errorf("oldest run: %v, want ErrNotFound", err)
	}
	if _, err := h.Store.Get(t.Context(), testRunID); err != nil {
		t.Errorf("new run: %v", err)
	}
}

// seedEndedRuns stores MaxScheduledRunsPerUnit ended runs of testUnit, so the
// next run that ends makes the oldest the 51st. It returns that oldest ID.
func seedEndedRuns(t *testing.T, h *fakeHooks) string {
	t.Helper()
	var oldest string
	for i := range jobs.MaxScheduledRunsPerUnit {
		id, err := jobs.UnitRunID(fmt.Sprintf("%032x", i+1))
		if err != nil {
			t.Fatal(err)
		}
		finished := testNow.Add(-time.Duration(i+1) * time.Minute)
		if err := h.Store.Create(t.Context(), jobs.Record{
			ID: id, Route: RouteScript, Target: []string{testUnit}, State: jobs.StateCompleted,
			CreatedAt: finished, StartedAt: finished, FinishedAt: finished, Protocol: jobs.Protocol,
		}); err != nil {
			t.Fatal(err)
		}
		oldest = id
	}
	return oldest
}

// endState reports, while End runs, which of its steps have happened.
func endState(h *fakeHooks, oldest string) string {
	ctx := context.Background()
	record, _ := h.Store.Get(ctx, testRunID)
	_, err := h.Store.Get(ctx, oldest)
	_, statErr := os.Stat(h.Store.OutputPath(testRunID))
	return fmt.Sprintf("record=%s pruned=%t output=%t", record.State, errors.Is(err, jobs.ErrNotFound), statErr == nil)
}

func TestEndPrunesThenCopiesTheOutputThenAlerts(t *testing.T) {
	h := newHooks(t)
	oldest := seedEndedRuns(t, h)
	var calls []string
	h.Output = func(_ context.Context, _ string, w io.Writer) error {
		calls = append(calls, "output: "+endState(h, oldest))
		_, _ = io.WriteString(w, "boom\n")
		return nil
	}
	h.Alert = func(_ context.Context, obs alerts.Observation) error {
		calls = append(calls, "alert: "+endState(h, oldest))
		h.alerts = append(h.alerts, obs)
		return nil
	}
	mustBegin(t, h, testUnit, testInvocation)
	if err := h.End(t.Context(), testUnit, testInvocation, Result{"exit-code", "exited", "3"}); err != nil {
		t.Fatalf("End: %v", err)
	}
	want := []string{
		"output: record=failed pruned=true output=false",
		"alert: record=failed pruned=true output=true",
	}
	if !slices.Equal(calls, want) {
		t.Errorf("calls = %q, want %q", calls, want)
	}
}

func TestEndCutsOffASlowOutputCopyAndStillAlerts(t *testing.T) {
	h := newHooks(t)
	limit := outputTimeout
	outputTimeout = 50 * time.Millisecond
	t.Cleanup(func() { outputTimeout = limit })
	h.Output = func(ctx context.Context, _ string, w io.Writer) error {
		_, _ = io.WriteString(w, "first\n")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
			return errors.New("the output copy was not cut off")
		}
	}
	mustBegin(t, h, testUnit, testInvocation)
	err := h.End(t.Context(), testUnit, testInvocation, Result{"exit-code", "exited", "3"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("End = %v, want the output copy's deadline", err)
	}
	if data, readErr := os.ReadFile(h.Store.OutputPath(testRunID)); readErr != nil || string(data) != "first\n" {
		t.Errorf("output = %q, %v; want what was read before the limit", data, readErr)
	}
	if record := mustGet(t, h, testRunID); record.State != jobs.StateFailed {
		t.Errorf("state = %q, want failed", record.State)
	}
	if len(h.alerts) != 1 {
		t.Errorf("alerts = %d, want 1 after the output copy was cut off", len(h.alerts))
	}
}

func TestEndAlertsLastAndOutlivesTheCallersContext(t *testing.T) {
	h := newHooks(t)
	oldest := seedEndedRuns(t, h)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	h.Output = func(_ context.Context, _ string, w io.Writer) error {
		_, _ = io.WriteString(w, "boom\n")
		cancel() // the caller's deadline passes during the copy
		return nil
	}
	var seen string
	h.Alert = func(alertCtx context.Context, obs alerts.Observation) error {
		seen = endState(h, oldest)
		time.Sleep(50 * time.Millisecond) // a slow delivery
		if err := alertCtx.Err(); err != nil {
			t.Errorf("the alert's context ended with the caller's: %v", err)
		}
		h.alerts = append(h.alerts, obs)
		return nil
	}
	mustBegin(t, h, testUnit, testInvocation)
	if err := h.End(ctx, testUnit, testInvocation, Result{"exit-code", "exited", "3"}); err != nil {
		t.Fatalf("End: %v", err)
	}
	if want := "record=failed pruned=true output=true"; seen != want {
		t.Errorf("when the alert was raised: %s, want %s", seen, want)
	}
	if len(h.alerts) != 1 {
		t.Errorf("alerts = %d, want 1 after the caller's context ended", len(h.alerts))
	}
}

func TestEndWithACanceledContextNeverPanics(t *testing.T) {
	h := newHooks(t)
	mustBegin(t, h, testUnit, testInvocation)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := h.End(ctx, testUnit, testInvocation, Result{"exit-code", "exited", "3"})
	record := mustGet(t, h, testRunID)
	switch {
	case err == nil:
		// The store wrote the record although the caller had given up.
		if record.State != jobs.StateFailed || len(h.alerts) != 1 {
			t.Errorf("End = nil with state %q and %d alerts; want failed and 1", record.State, len(h.alerts))
		}
	case errors.Is(err, context.Canceled):
		// The store refused the write: nothing else happened.
		if record.State != jobs.StateRunning || len(h.alerts) != 0 {
			t.Errorf("End = %v with state %q and %d alerts; want running and none", err, record.State, len(h.alerts))
		}
	default:
		t.Fatalf("End = %v, want nil or the store's context.Canceled", err)
	}
}

func TestEndAlertsWithTheLabelBeginRecorded(t *testing.T) {
	h := newHooks(t)
	mustBegin(t, h, testUnit, testInvocation)
	if err := os.Remove(filepath.Join(h.ScheduleDir, "definitions", testScheduleID+".json")); err != nil {
		t.Fatal(err)
	}
	if err := h.End(t.Context(), testUnit, testInvocation, Result{"exit-code", "exited", "3"}); err != nil {
		t.Fatal(err)
	}
	if len(h.alerts) != 1 || h.alerts[0].Title != "Nightly backup failed" {
		t.Errorf("alerts = %+v, want one titled %q", h.alerts, "Nightly backup failed")
	}
}

func TestEndIgnoresARecordThatEndedWhileItWasBeingCreated(t *testing.T) {
	h := newHooks(t)
	failed := Result{"exit-code", "exited", "3"}
	// End found no record; the boot ID is read before it creates one. Let a
	// racing End finish the run there.
	h.BootID = func() (string, error) {
		h.BootID = func() (string, error) { return testBoot, nil }
		racing := h.Hooks
		if err := racing.End(t.Context(), testUnit, testInvocation, failed); err != nil {
			t.Errorf("racing End: %v", err)
		}
		return testBoot, nil
	}
	if err := h.End(t.Context(), testUnit, testInvocation, failed); err != nil {
		t.Fatalf("End: %v", err)
	}
	if len(h.alerts) != 1 {
		t.Errorf("alerts = %d, want 1: the run ended once", len(h.alerts))
	}
}

func TestRequestCancelMarksTheUnitsRunningRecord(t *testing.T) {
	h := newHooks(t)
	const otherInvocation = "fedcba0987654321fedcba0987654321"
	const otherRunID = "fedcba09-8765-4321-fedc-ba0987654321"
	const endedInvocation = "00000000000000000000000000000001"
	const endedRunID = "00000000-0000-0000-0000-000000000001"
	const otherUnit = "linuxio-docker-update.service"

	mustBegin(t, h, testUnit, endedInvocation)
	if err := h.End(t.Context(), testUnit, endedInvocation, Result{Service: "success"}); err != nil {
		t.Fatalf("End: %v", err)
	}
	mustBegin(t, h, testUnit, testInvocation)
	mustBegin(t, h, otherUnit, otherInvocation)

	for _, invocation := range []string{testInvocation, ""} {
		if _, err := h.Store.Update(t.Context(), testRunID, func(r *jobs.Record) error {
			r.CancelRequested = false
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := RequestCancel(t.Context(), h.Store, testUnit, invocation); err != nil {
			t.Fatalf("RequestCancel(%q): %v", invocation, err)
		}
		if !mustGet(t, h, testRunID).CancelRequested {
			t.Errorf("RequestCancel(%q) did not mark the running record of the unit", invocation)
		}
		if mustGet(t, h, otherRunID).CancelRequested {
			t.Errorf("RequestCancel(%q) marked the running record of another unit", invocation)
		}
		if ended := mustGet(t, h, endedRunID); ended.CancelRequested || ended.State != jobs.StateCompleted {
			t.Errorf("RequestCancel(%q) changed the ended record: %+v", invocation, ended)
		}
	}
}

func TestRequestCancelOfAStaleInvocationMarksNothing(t *testing.T) {
	h := newHooks(t)
	const endedInvocation = "00000000000000000000000000000001"
	mustBegin(t, h, testUnit, endedInvocation)
	if err := h.End(t.Context(), testUnit, endedInvocation, Result{Service: "success"}); err != nil {
		t.Fatalf("End: %v", err)
	}
	// The next run of the unit has started when the Stop of the first arrives.
	mustBegin(t, h, testUnit, testInvocation)
	if err := RequestCancel(t.Context(), h.Store, testUnit, endedInvocation); err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}
	if mustGet(t, h, testRunID).CancelRequested {
		t.Error("a Stop of an ended run marked the next run")
	}
}

func TestWithdrawCancelClearsTheMarkOfTheRun(t *testing.T) {
	h := newHooks(t)
	const otherInvocation = "fedcba0987654321fedcba0987654321"
	const otherRunID = "fedcba09-8765-4321-fedc-ba0987654321"
	const otherUnit = "linuxio-docker-update.service"
	mustBegin(t, h, testUnit, testInvocation)
	mustBegin(t, h, otherUnit, otherInvocation)
	for _, unit := range []string{testUnit, otherUnit} {
		if err := RequestCancel(t.Context(), h.Store, unit, ""); err != nil {
			t.Fatal(err)
		}
	}

	if err := WithdrawCancel(t.Context(), h.Store, testUnit, otherInvocation); err != nil {
		t.Fatalf("WithdrawCancel of another invocation: %v", err)
	}
	if !mustGet(t, h, testRunID).CancelRequested {
		t.Error("withdrawing another invocation cleared the mark")
	}
	if err := WithdrawCancel(t.Context(), h.Store, testUnit, testInvocation); err != nil {
		t.Fatalf("WithdrawCancel: %v", err)
	}
	if mustGet(t, h, testRunID).CancelRequested {
		t.Error("the mark of the run was not withdrawn")
	}
	if !mustGet(t, h, otherRunID).CancelRequested {
		t.Error("the mark of another unit's run was withdrawn")
	}
	if err := WithdrawCancel(t.Context(), h.Store, otherUnit, ""); err != nil {
		t.Fatalf("WithdrawCancel without an invocation: %v", err)
	}
	if mustGet(t, h, otherRunID).CancelRequested {
		t.Error("withdrawing without an invocation left the unit's mark")
	}
}

func TestWithdrawCancelLeavesAnEndedRun(t *testing.T) {
	h := newHooks(t)
	mustBegin(t, h, testUnit, testInvocation)
	if err := RequestCancel(t.Context(), h.Store, testUnit, testInvocation); err != nil {
		t.Fatal(err)
	}
	if err := h.End(t.Context(), testUnit, testInvocation, Result{"signal", "killed", "TERM"}); err != nil {
		t.Fatal(err)
	}
	if err := WithdrawCancel(t.Context(), h.Store, testUnit, testInvocation); err != nil {
		t.Fatalf("WithdrawCancel: %v", err)
	}
	if record := mustGet(t, h, testRunID); !record.CancelRequested || record.State != jobs.StateCanceled {
		t.Errorf("record = %q, cancel requested %t; want canceled and still marked", record.State, record.CancelRequested)
	}
}

func TestRequestCancelWithoutARunIsNil(t *testing.T) {
	h := newHooks(t)
	if err := RequestCancel(t.Context(), h.Store, testUnit, testInvocation); err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}
	if err := WithdrawCancel(t.Context(), h.Store, testUnit, testInvocation); err != nil {
		t.Fatalf("WithdrawCancel: %v", err)
	}
}

// resolving makes h collect the alerts it resolves.
func resolving(h *fakeHooks) *[]string {
	var resolved []string
	h.Resolve = func(_ context.Context, source, key string) error {
		resolved = append(resolved, source+"/"+key)
		return nil
	}
	return &resolved
}

// A worker (snapshots, resync, Docker updates) raises an alert for each rule,
// job, pool or container that fails and then exits with ExitReported. A run
// that failed in any other way, such as a worker that could not read its
// configuration, reach Docker or stay within its memory, reports here
// instead, once per unit.
func TestFailedWorkerRunRaisesTheRunAlert(t *testing.T) {
	const pool = "0a1b2c3d-1111-2222-3333-444455556666"
	for _, tt := range []struct {
		unit, key, title string
		result           Result
		message          string
		occurrence       string
	}{
		{"linuxio-docker-update.service", "run-docker-update", "Container updates failed", Result{"exit-code", "exited", "1"}, "Exited with status 1.", "exited with status 1"},
		{"linuxio-btrfs-snapshot.service", "run-btrfs-snapshot", "Snapshots and replication failed", Result{"oom-kill", "killed", "KILL"}, "Killed for using too much memory.", "killed for using too much memory"},
		{"linuxio-btrfs-resync@" + pool + ".service", "run-btrfs-resync-" + pool, "Pool resync failed", Result{"signal", "killed", "SEGV"}, "Killed by signal SEGV.", "killed by signal SEGV"},
		{"linuxio-docker-update.service", "run-docker-update", "Container updates failed", Result{"timeout", "killed", "TERM"}, "Timed out.", "timed out"},
	} {
		h := newHooks(t)
		resolved := resolving(h)
		mustBegin(t, h, tt.unit, testInvocation)
		if err := h.End(t.Context(), tt.unit, testInvocation, tt.result); err != nil {
			t.Fatal(err)
		}
		// The occurrence is how the run failed: a unit that keeps failing
		// the same way is delivered once, and its link follows the latest run.
		want := alerts.Observation{
			Source: "jobs", Key: tt.key, Severity: alerts.SeverityError,
			Title: tt.title, Message: tt.message + " The run's output is in the history.",
			Link: "/history?job=" + testRunID, OccurrenceID: tt.occurrence,
		}
		if len(h.alerts) != 1 || h.alerts[0] != want || len(*resolved) != 0 {
			t.Errorf("%s: alerts = %+v, resolved = %v; want [%+v] and nothing resolved", tt.unit, h.alerts, *resolved, want)
		}
	}
}

// The btrfs health check finds the pool of a resync run alert from the key
// the end hook raised, and no other run or job alert names a pool.
func TestResyncRunAlertPoolReadsTheKeyTheEndHookRaises(t *testing.T) {
	const pool = "0a1b2c3d-1111-2222-3333-444455556666"
	const unit = "linuxio-btrfs-resync@" + pool + ".service"
	h := newHooks(t)
	mustBegin(t, h, unit, testInvocation)
	if err := h.End(t.Context(), unit, testInvocation, Result{"exit-code", "exited", "1"}); err != nil {
		t.Fatal(err)
	}
	if len(h.alerts) != 1 || h.alerts[0].Source != AlertSource {
		t.Fatalf("alerts = %+v, want the run alert", h.alerts)
	}
	if got, ok := ResyncRunAlertPool(h.alerts[0].Key); !ok || got != pool {
		t.Errorf("ResyncRunAlertPool(%q) = %q, %v; want %q", h.alerts[0].Key, got, ok, pool)
	}
	for _, key := range []string{"run-docker-update", "run-btrfs-snapshot", "run-btrfs-resync", "run-btrfs-resync-", "run-btrfs-resync-not-a-uuid", testRunID, FinishedAlertKey(pool)} {
		if got, ok := ResyncRunAlertPool(key); ok {
			t.Errorf("ResyncRunAlertPool(%q) = %q; want no pool", key, got)
		}
	}
}

// A run that completed, and one whose worker reported every failure itself,
// raise nothing and clear the run alert of an earlier run.
func TestWorkerRunWithNothingLeftToReportClearsTheRunAlert(t *testing.T) {
	const unit = "linuxio-docker-update.service"
	for name, result := range map[string]Result{
		"completed":            {"success", "exited", "0"},
		"failed, all reported": {"exit-code", "exited", strconv.Itoa(ExitReported)},
	} {
		h := newHooks(t)
		resolved := resolving(h)
		mustBegin(t, h, unit, testInvocation)
		if err := h.End(t.Context(), unit, testInvocation, result); err != nil {
			t.Fatal(err)
		}
		if len(h.alerts) != 0 || len(*resolved) != 1 || (*resolved)[0] != "jobs/run-docker-update" {
			t.Errorf("%s: alerts = %+v, resolved = %v; want only the run alert resolved", name, h.alerts, *resolved)
		}
	}
	// A script that exits with the same status is a failed script.
	h := newHooks(t)
	resolved := resolving(h)
	mustBegin(t, h, testUnit, testInvocation)
	if err := h.End(t.Context(), testUnit, testInvocation, Result{"exit-code", "exited", strconv.Itoa(ExitReported)}); err != nil {
		t.Fatal(err)
	}
	if len(h.alerts) != 1 || h.alerts[0].Key != testRunID || len(*resolved) != 0 {
		t.Errorf("script: alerts = %+v, resolved = %v; want its own failure alert", h.alerts, *resolved)
	}
}

// A worker run that a shutdown stopped, or that was canceled, is not a
// failure: its timer runs it again, and a standing run alert stays.
func TestStoppedWorkerRunChangesNoRunAlert(t *testing.T) {
	const unit = "linuxio-btrfs-snapshot.service"
	killed := Result{"signal", "killed", "TERM"}
	h := newHooks(t)
	resolved := resolving(h)
	h.stopping = true
	mustBegin(t, h, unit, testInvocation)
	if err := h.End(t.Context(), unit, testInvocation, killed); err != nil {
		t.Fatal(err)
	}
	h2 := newHooks(t)
	resolved2 := resolving(h2)
	mustBegin(t, h2, unit, testInvocation)
	requestCancel(t, h2)
	if err := h2.End(t.Context(), unit, testInvocation, killed); err != nil {
		t.Fatal(err)
	}
	// A run that did no work, such as a Docker pass that found a manual
	// update holding the lock.
	h3 := newHooks(t)
	resolved3 := resolving(h3)
	mustBegin(t, h3, unit, testInvocation)
	if err := h3.End(t.Context(), unit, testInvocation, Result{"exit-code", "exited", strconv.Itoa(ExitSkipped)}); err != nil {
		t.Fatal(err)
	}
	if len(h.alerts)+len(h2.alerts)+len(h3.alerts)+len(*resolved)+len(*resolved2)+len(*resolved3) != 0 {
		t.Errorf("alerts = %+v %+v %+v, resolved = %v %v %v; want none", h.alerts, h2.alerts, h3.alerts, *resolved, *resolved2, *resolved3)
	}
}

// The provider's automatic update is recorded like any run, but it is not one
// of LinuxIO's workers: its own hook reports a failed run ("Automatic updates
// failed"), so the end hook neither raises nor clears a run alert for it.
func TestSystemUpdateRunIsRecordedWithoutARunAlert(t *testing.T) {
	const unit = "apt-daily-upgrade.service"
	for name, tt := range map[string]struct {
		result Result
		state  jobs.State
	}{
		"completed": {Result{"success", "exited", "0"}, jobs.StateCompleted},
		"failed":    {Result{"exit-code", "exited", "100"}, jobs.StateFailed},
	} {
		h := newHooks(t)
		resolved := resolving(h)
		mustBegin(t, h, unit, testInvocation)
		if record := mustGet(t, h, testRunID); record.Route != RouteSystemUpdate || record.State != jobs.StateRunning {
			t.Fatalf("%s: running record = %s/%s, want %s running", name, record.Route, record.State, RouteSystemUpdate)
		}
		if err := h.End(t.Context(), unit, testInvocation, tt.result); err != nil {
			t.Fatal(err)
		}
		if record := mustGet(t, h, testRunID); record.State != tt.state {
			t.Errorf("%s: record = %s, want %s", name, record.State, tt.state)
		}
		if len(h.alerts) != 0 || len(*resolved) != 0 {
			t.Errorf("%s: alerts = %+v, resolved = %v; want neither", name, h.alerts, *resolved)
		}
	}
}
