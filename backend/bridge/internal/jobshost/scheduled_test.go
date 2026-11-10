package jobshost

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

const (
	testScheduleID = "6f1c3a52-8d2e-4b7a-9c41-2e5d7f8a9b10"
	invocationA    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1"
	invocationB    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa2"
	invocationC    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa3"
	invocationD    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa4"
	invocationE    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa5"
	scriptUnit     = "linuxio-schedule-" + testScheduleID + ".service"
)

// scheduledHost is a test host whose scheduled store is dir, with the tasks
// routes registered and tasks.events polling every 10 ms. active answers for
// the units; nil says every run still runs.
func scheduledHost(t *testing.T, dir string, active func(ctx context.Context, unit, invocationID string) (bool, error)) *testHost {
	t.Helper()
	poll := scheduledPoll
	scheduledPoll = 10 * time.Millisecond
	t.Cleanup(func() { scheduledPoll = poll })
	if active == nil {
		active = func(context.Context, string, string) (bool, error) { return true, nil }
	}
	th := newTestHost(t, func(c *Config) {
		c.Scheduled = jobs.NewStore(filepath.Join(dir, "state"), filepath.Join(dir, "run"))
		c.UnitActive = active
	})
	th.cfg.Tasks.RegisterRoutes(th.cfg.Router)
	return th
}

func thisBoot(t *testing.T) string {
	t.Helper()
	boot, err := jobs.BootID()
	if err != nil {
		t.Fatalf("BootID: %v", err)
	}
	return boot
}

// scriptRun is the running record of a run of the "Nightly backup" schedule
// that has invocation, begun in boot.
func scriptRun(t *testing.T, invocation, boot string) jobs.Record {
	t.Helper()
	id, err := jobs.UnitRunID(invocation)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	return jobs.Record{
		ID: id, Route: scheduledrun.RouteScript, Owner: jobs.Owner{UID: 0, Username: "root"},
		Target: []string{scriptUnit}, State: jobs.StateRunning, CreatedAt: now, StartedAt: now,
		Metadata: jsontext.Value(`{"label":"Nightly backup","identity":["` + testScheduleID + `"]}`),
		Executor: jobs.Executor{Kind: jobs.ExecutorUnit, Unit: scriptUnit, InvocationID: invocation, BootID: boot},
		Protocol: jobs.Protocol,
	}
}

func createRuns(t *testing.T, store *jobs.Store, records ...jobs.Record) {
	t.Helper()
	for _, record := range records {
		if err := store.Create(context.Background(), record); err != nil {
			t.Fatalf("Create(%s): %v", record.ID, err)
		}
	}
}

// endRun ends a scheduled run's record as its end hook would.
func endRun(t *testing.T, store *jobs.Store, id string, state jobs.State, reason jobs.Reason, jobErr *jobs.Error) {
	t.Helper()
	if _, err := store.Update(context.Background(), id, func(r *jobs.Record) error {
		r.State, r.Reason, r.Error, r.FinishedAt = state, reason, jobErr, time.Now().UTC()
		return nil
	}); err != nil {
		t.Fatalf("Update(%s): %v", id, err)
	}
}

func writeRunProgress(t *testing.T, store *jobs.Store, id, progress string) {
	t.Helper()
	if err := store.WriteProgress(id, jsontext.Value(progress)); err != nil {
		t.Fatalf("WriteProgress(%s): %v", id, err)
	}
}

type taskEvent struct {
	Type     bridgeipc.TaskEventType `json:"type"`
	Task     bridgeipc.TaskSnapshot  `json:"task"`
	Progress *bridgeipc.TaskProgress `json:"progress"`
	Error    *bridgeipc.Error        `json:"error"`
}

func readEvent(t *testing.T, conn net.Conn) taskEvent {
	t.Helper()
	var event taskEvent
	payload := readFrame(t, conn, relay.OpStreamProgress)
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatalf("json.Unmarshal(event %s): %v", payload, err)
	}
	return event
}

// readEvents reads n events and returns them by task ID.
func readEvents(t *testing.T, conn net.Conn, n int) map[string]taskEvent {
	t.Helper()
	events := make(map[string]taskEvent, n)
	for range n {
		event := readEvent(t, conn)
		if _, dup := events[event.Task.ID]; dup {
			t.Fatalf("second %s event for %s", event.Type, event.Task.ID)
		}
		events[event.Task.ID] = event
	}
	return events
}

func callTasks(t *testing.T, sess *relay.YamuxSession, route, id string) relay.ResultFrame {
	t.Helper()
	request := map[string]string{}
	if id != "" {
		request["taskId"] = id
	}
	return readResult(t, openStream(t, sess, route, request, ""))
}

func decodeSnapshot(t *testing.T, result relay.ResultFrame) bridgeipc.TaskSnapshot {
	t.Helper()
	var snapshot bridgeipc.TaskSnapshot
	if result.Status != "ok" {
		t.Fatalf("result = %#v, want ok", result)
	}
	if err := json.Unmarshal(result.Data, &snapshot); err != nil {
		t.Fatalf("json.Unmarshal(snapshot %s): %v", result.Data, err)
	}
	return snapshot
}

func TestSnapshotFromRecordCarriesMetadata(t *testing.T) {
	record := scriptRun(t, invocationA, "a-boot")
	record.Metadata = jsontext.Value(`{"label":"x","identity":["a"]}`)
	snapshot := snapshotFromRecord(record)
	if snapshot.Metadata == nil || snapshot.Metadata.Label != "x" || !slices.Equal(snapshot.Metadata.Identity, []string{"a"}) {
		t.Fatalf("metadata = %+v, want label x and identity [a]", snapshot.Metadata)
	}
}

func TestTasksListIncludesRunningScheduledRuns(t *testing.T) {
	th := scheduledHost(t, t.TempDir(), nil)
	boot := thisBoot(t)
	running, ended := scriptRun(t, invocationA, boot), scriptRun(t, invocationB, boot)
	createRuns(t, th.cfg.Scheduled, running, ended)
	writeRunProgress(t, th.cfg.Scheduled, running.ID, `{"message":"x"}`)
	endRun(t, th.cfg.Scheduled, ended.ID, jobs.StateCompleted, jobs.ReasonNone, nil)
	th.start()

	result := callTasks(t, th.connect(), "tasks.list", "")
	var snapshots []bridgeipc.TaskSnapshot
	if result.Status != "ok" || json.Unmarshal(result.Data, &snapshots) != nil {
		t.Fatalf("tasks.list = %#v", result)
	}
	if len(snapshots) != 1 {
		t.Fatalf("tasks.list = %s, want the running run only", result.Data)
	}
	got := snapshots[0]
	if got.ID != running.ID || got.Type != "scheduled.script" || got.State != bridgeipc.TaskStateRunning {
		t.Errorf("snapshot = %s %s %s, want %s scheduled.script running", got.ID, got.Type, got.State, running.ID)
	}
	if got.Metadata == nil || got.Metadata.Label != "Nightly backup" {
		t.Errorf("metadata = %+v, want label Nightly backup", got.Metadata)
	}
	if got.Progress == nil || got.Progress.Message != "x" {
		t.Errorf("progress = %+v, want message x", got.Progress)
	}
}

func TestTasksEventsFollowAScheduledRun(t *testing.T) {
	th := scheduledHost(t, t.TempDir(), nil)
	boot := thisBoot(t)
	first, second := scriptRun(t, invocationA, boot), scriptRun(t, invocationB, boot)
	createRuns(t, th.cfg.Scheduled, first)
	th.start()
	events := openStream(t, th.connect(), "tasks.events", bridgeipc.TaskEventsRequest{}, "")

	if event := readEvent(t, events); event.Type != bridgeipc.TaskEventSnapshot || event.Task.ID != first.ID || event.Task.State != bridgeipc.TaskStateRunning {
		t.Fatalf("first event = %s %s %s, want a running snapshot of %s", event.Type, event.Task.ID, event.Task.State, first.ID)
	}

	writeRunProgress(t, th.cfg.Scheduled, first.ID, `{"percentage":40,"message":"copying"}`)
	event := readEvent(t, events)
	if event.Type != bridgeipc.TaskEventProgress || event.Task.ID != first.ID {
		t.Fatalf("event = %s %s, want progress of %s", event.Type, event.Task.ID, first.ID)
	}
	if event.Task.Progress == nil || event.Task.Progress.Message != "copying" || event.Progress == nil || event.Progress.Message != "copying" {
		t.Fatalf("progress = %+v in task %+v, want message copying in both", event.Progress, event.Task.Progress)
	}

	createRuns(t, th.cfg.Scheduled, second)
	if event = readEvent(t, events); event.Type != bridgeipc.TaskEventSnapshot || event.Task.ID != second.ID {
		t.Fatalf("event = %s %s, want a snapshot of the new run %s", event.Type, event.Task.ID, second.ID)
	}

	endRun(t, th.cfg.Scheduled, first.ID, jobs.StateFailed, jobs.ReasonNone, &jobs.Error{Code: 500, Message: "exited with status 3"})
	event = readEvent(t, events)
	if event.Type != bridgeipc.TaskEventError || event.Task.ID != first.ID || event.Task.State != bridgeipc.TaskStateFailed {
		t.Fatalf("event = %s %s %s, want task.error of the failed %s", event.Type, event.Task.ID, event.Task.State, first.ID)
	}
	if event.Task.Error == nil || event.Task.Error.Message != "exited with status 3" || event.Error == nil || event.Error.Message != "exited with status 3" {
		t.Fatalf("error = %+v in task %+v, want the record's message", event.Error, event.Task.Error)
	}
}

func TestTasksEventsEndWithCompletedAndCanceled(t *testing.T) {
	dir := t.TempDir()
	// The unit of invocationE dies without its end hook once died is set.
	var died atomic.Bool
	th := scheduledHost(t, dir, func(_ context.Context, _, invocationID string) (bool, error) {
		return invocationID != invocationE || !died.Load(), nil
	})
	boot := thisBoot(t)
	completed, canceled := scriptRun(t, invocationA, boot), scriptRun(t, invocationB, boot)
	interrupted, gone, crashed := scriptRun(t, invocationC, boot), scriptRun(t, invocationD, boot), scriptRun(t, invocationE, boot)
	createRuns(t, th.cfg.Scheduled, completed, canceled, interrupted, gone, crashed)
	th.start()
	events := openStream(t, th.connect(), "tasks.events", bridgeipc.TaskEventsRequest{}, "")
	for id, event := range readEvents(t, events, 5) {
		if event.Type != bridgeipc.TaskEventSnapshot {
			t.Fatalf("event of %s = %s, want a snapshot", id, event.Type)
		}
	}

	died.Store(true)
	endRun(t, th.cfg.Scheduled, completed.ID, jobs.StateCompleted, jobs.ReasonNone, nil)
	endRun(t, th.cfg.Scheduled, canceled.ID, jobs.StateCanceled, jobs.ReasonNone, &jobs.Error{Code: 499, Message: "operation aborted"})
	endRun(t, th.cfg.Scheduled, interrupted.ID, jobs.StateInterrupted, jobs.ReasonShutdown, nil)
	if err := os.Remove(filepath.Join(dir, "state", gone.ID+".json")); err != nil {
		t.Fatal(err)
	}

	ended := readEvents(t, events, 5)
	if event := ended[completed.ID]; event.Type != bridgeipc.TaskEventResult || event.Task.State != bridgeipc.TaskStateCompleted {
		t.Errorf("completed run: %s %s, want task.result completed", event.Type, event.Task.State)
	}
	if event := ended[canceled.ID]; event.Type != bridgeipc.TaskEventCanceled || event.Task.State != bridgeipc.TaskStateCanceled {
		t.Errorf("canceled run: %s %s, want task.canceled canceled", event.Type, event.Task.State)
	}
	for _, id := range []string{interrupted.ID, gone.ID, crashed.ID} {
		event := ended[id]
		if event.Type != bridgeipc.TaskEventError || event.Task.State != bridgeipc.TaskStateInterrupted ||
			event.Error == nil || event.Error.Code != 409 || event.Task.Error == nil || event.Task.Error.Code != 409 {
			t.Errorf("run %s: %s %s error %+v, want task.error interrupted with 409", id, event.Type, event.Task.State, event.Error)
		}
	}
	if r, err := th.cfg.Scheduled.Get(context.Background(), crashed.ID); err != nil || r.State != jobs.StateInterrupted || r.Reason != jobs.ReasonCrash {
		t.Errorf("record of the run whose unit died = %+v, %v, want interrupted/crash", r, err)
	}
}

func TestTasksGetAnswersForAScheduledRun(t *testing.T) {
	th := scheduledHost(t, t.TempDir(), nil)
	boot := thisBoot(t)
	running, failed := scriptRun(t, invocationA, boot), scriptRun(t, invocationB, boot)
	createRuns(t, th.cfg.Scheduled, running, failed)
	writeRunProgress(t, th.cfg.Scheduled, running.ID, `{"message":"x"}`)
	endRun(t, th.cfg.Scheduled, failed.ID, jobs.StateFailed, jobs.ReasonNone, &jobs.Error{Code: 500, Message: "exited with status 3"})
	th.start()
	sess := th.connect()

	got := decodeSnapshot(t, callTasks(t, sess, "tasks.get", running.ID))
	if got.State != bridgeipc.TaskStateRunning || got.Type != "scheduled.script" || got.Metadata == nil ||
		got.Metadata.Label != "Nightly backup" || got.Progress == nil || got.Progress.Message != "x" {
		t.Errorf("running run = %+v, want it running with its label and progress", got)
	}
	got = decodeSnapshot(t, callTasks(t, sess, "tasks.get", failed.ID))
	if got.State != bridgeipc.TaskStateFailed || got.Error == nil || got.Error.Message != "exited with status 3" {
		t.Errorf("failed run = %+v, want failed with the record's message", got)
	}
}

func TestTasksGetReportsADeadScheduledRunInterrupted(t *testing.T) {
	th := scheduledHost(t, t.TempDir(), func(context.Context, string, string) (bool, error) { return false, nil })
	th.start()
	// Begun after the host start, so only tasks.get checks it with systemd.
	run := scriptRun(t, invocationA, thisBoot(t))
	createRuns(t, th.cfg.Scheduled, run)

	if got := decodeSnapshot(t, callTasks(t, th.connect(), "tasks.get", run.ID)); got.State != bridgeipc.TaskStateInterrupted {
		t.Errorf("tasks.get = %s, want interrupted", got.State)
	}
	if r, err := th.cfg.Scheduled.Get(context.Background(), run.ID); err != nil || r.State != jobs.StateInterrupted || r.Reason != jobs.ReasonCrash {
		t.Errorf("record = %+v, %v, want interrupted/crash", r, err)
	}
}

func TestTasksCancelRefusesAScheduledRun(t *testing.T) {
	th := scheduledHost(t, t.TempDir(), nil)
	boot := thisBoot(t)
	running, ended := scriptRun(t, invocationA, boot), scriptRun(t, invocationB, boot)
	createRuns(t, th.cfg.Scheduled, running, ended)
	endRun(t, th.cfg.Scheduled, ended.ID, jobs.StateCompleted, jobs.ReasonNone, nil)
	th.start()
	sess := th.connect()

	result := callTasks(t, sess, "tasks.cancel", running.ID)
	if result.Status != "error" || result.Code != 409 || result.Error != "a scheduled run is stopped on its page" {
		t.Fatalf("tasks.cancel = %#v, want 409 a scheduled run is stopped on its page", result)
	}
	if r, err := th.cfg.Scheduled.Get(context.Background(), running.ID); err != nil || r.State != jobs.StateRunning || r.CancelRequested {
		t.Fatalf("record = %+v, %v, want it running without a cancel request", r, err)
	}
	if got := decodeSnapshot(t, callTasks(t, sess, "tasks.cancel", ended.ID)); got.State != bridgeipc.TaskStateCompleted {
		t.Errorf("tasks.cancel of an ended run = %s, want its completed snapshot", got.State)
	}
}

func TestTasksWatchRefusesARunningScheduledRun(t *testing.T) {
	th := scheduledHost(t, t.TempDir(), nil)
	boot := thisBoot(t)
	running, failed := scriptRun(t, invocationA, boot), scriptRun(t, invocationB, boot)
	createRuns(t, th.cfg.Scheduled, running, failed)
	endRun(t, th.cfg.Scheduled, failed.ID, jobs.StateFailed, jobs.ReasonNone, &jobs.Error{Code: 500, Message: "exited with status 3"})
	th.start()
	sess := th.connect()

	result := callTasks(t, sess, "tasks.watch", running.ID)
	if result.Status != "error" || result.Code != 409 || result.Error != "a running scheduled run is followed through tasks.events" {
		t.Fatalf("tasks.watch = %#v, want 409 a running scheduled run is followed through tasks.events", result)
	}
	watch := openStream(t, sess, "tasks.watch", map[string]string{"taskId": failed.ID}, "")
	if snapshot := readSnapshot(t, watch); snapshot.State != bridgeipc.TaskStateFailed {
		t.Fatalf("watch snapshot = %s, want failed", snapshot.State)
	}
	if result := readResult(t, watch); result.Status != "error" || result.Error != "exited with status 3" {
		t.Fatalf("watch result = %#v, want the stored failure", result)
	}
}

func TestHostWithoutAScheduledStoreListsNone(t *testing.T) {
	dir := t.TempDir()
	scheduled := jobs.NewStore(filepath.Join(dir, "state"), filepath.Join(dir, "run"))
	run := scriptRun(t, invocationA, thisBoot(t))
	createRuns(t, scheduled, run)
	th := newTestHost(t, nil)
	th.cfg.Tasks.RegisterRoutes(th.cfg.Router)
	th.start()
	sess := th.connect()

	result := callTasks(t, sess, "tasks.list", "")
	var snapshots []bridgeipc.TaskSnapshot
	if result.Status != "ok" || json.Unmarshal(result.Data, &snapshots) != nil || len(snapshots) != 0 {
		t.Fatalf("tasks.list = %#v, want no tasks", result)
	}
	if result := callTasks(t, sess, "tasks.get", run.ID); result.Code != 404 {
		t.Fatalf("tasks.get of a scheduled run = %#v, want 404", result)
	}
}

func TestHostStartMarksScheduledRunsOfAnEarlierBoot(t *testing.T) {
	var asked atomic.Int32
	th := scheduledHost(t, t.TempDir(), func(context.Context, string, string) (bool, error) {
		asked.Add(1)
		return true, nil
	})
	run := scriptRun(t, invocationA, "an-earlier-boot")
	createRuns(t, th.cfg.Scheduled, run)
	th.start()

	record, err := th.cfg.Scheduled.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != jobs.StateInterrupted || record.Reason != jobs.ReasonReboot {
		t.Fatalf("record = %s/%s, want interrupted/reboot", record.State, record.Reason)
	}
	if n := asked.Load(); n != 0 {
		t.Errorf("systemd asked %d times about a run of an earlier boot", n)
	}
	got := th.observed()
	if len(got) != 1 || got[0].Key != run.ID || got[0].Title != "Nightly backup was interrupted" ||
		got[0].Link != "/services/schedules?schedule="+testScheduleID {
		t.Fatalf("alerts = %#v, want one interrupted alert for %s", got, run.ID)
	}
}

func TestScheduledEventsListWhenTheStoreMayHaveChanged(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	old, written, ahead := now.Add(-time.Hour), now.Add(4*time.Second-10*time.Millisecond), now.Add(time.Hour)
	var f scheduledEvents
	for _, step := range []struct {
		name    string
		at      time.Duration
		modTime time.Time
		known   bool
		want    bool
	}{
		{"the first poll", 0, old, true, true},
		{"nothing changed", 2 * time.Second, old, true, false},
		{"a record was written", 4 * time.Second, written, true, true},
		{"the last listing read a ModTime too young to trust", 6 * time.Second, written, true, true},
		{"the ModTime is old enough", 8 * time.Second, written, true, false},
		{"the ModTime cannot be read", 10 * time.Second, time.Time{}, false, false},
		{"30 s since the last listing", 36 * time.Second, written, true, true},
		{"a ModTime ahead of the clock", 40 * time.Second, ahead, true, true},
		{"still ahead, nothing changed", 42 * time.Second, ahead, true, false},
	} {
		if got := f.dueAt(now.Add(step.at), step.modTime, step.known); got != step.want {
			t.Fatalf("%s: due = %v, want %v", step.name, got, step.want)
		}
	}
}

func TestScheduledEventsListAgainAfterAFailedListing(t *testing.T) {
	dir := t.TempDir()
	// A file where the state directory belongs: listing it fails.
	state := filepath.Join(dir, "state")
	if err := os.WriteFile(state, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(state, old, old); err != nil {
		t.Fatal(err)
	}
	h := newHost(Config{Scheduled: jobs.NewStore(state, filepath.Join(dir, "run")), Now: time.Now})
	f := &scheduledEvents{s: storedJobs{h}, runs: make(map[string]jobs.Record)}

	if !f.poll(t.Context()) {
		t.Fatal("poll stopped the stream")
	}
	if !f.due() {
		t.Fatal("the poll after a failed listing does not list again")
	}
}

// callHistory sends a tasks.history request and decodes the history it answers.
func callHistory(t *testing.T, sess *relay.YamuxSession, request any) bridgeipc.TaskHistory {
	t.Helper()
	result := readResult(t, openStream(t, sess, "tasks.history", request, ""))
	var history bridgeipc.TaskHistory
	if result.Status != "ok" {
		t.Fatalf("tasks.history = %#v, want ok", result)
	}
	if err := json.Unmarshal(result.Data, &history); err != nil {
		t.Fatalf("json.Unmarshal(history %s): %v", result.Data, err)
	}
	return history
}

func historyIDs(history bridgeipc.TaskHistory) []string {
	ids := make([]string, len(history.Entries))
	for i, entry := range history.Entries {
		ids[i] = entry.Task.ID
	}
	return ids
}

// endedHostJob creates the record of a host job that ended at finished.
func endedHostJob(t *testing.T, th *testHost, id string, finished time.Time, label string) jobs.Record {
	t.Helper()
	record := jobs.Record{
		ID: id, Route: "test.job", Owner: jobs.Owner{UID: th.cfg.UID, Username: "alice"},
		State: jobs.StateCompleted, CreatedAt: finished.Add(-time.Minute), StartedAt: finished.Add(-time.Minute), FinishedAt: finished,
		Metadata: jsontext.Value(`{"label":"` + label + `"}`),
		Request:  jsontext.Value(`{"secret":"x"}`), Executor: jobs.Executor{Kind: "host"}, Protocol: jobs.Protocol,
	}
	createRuns(t, th.cfg.Store, record)
	return record
}

// endedScriptRun creates the record of a scheduled run of unit that ended at
// finished.
func endedScriptRun(t *testing.T, store *jobs.Store, invocation, unit string, finished time.Time) jobs.Record {
	t.Helper()
	record := scriptRun(t, invocation, "a-boot")
	record.Target, record.Executor.Unit = []string{unit}, unit
	record.State, record.CreatedAt, record.StartedAt, record.FinishedAt = jobs.StateCompleted, finished.Add(-time.Minute), finished.Add(-time.Minute), finished
	createRuns(t, store, record)
	return record
}

func TestTasksHistoryListsEndedJobsNewestFirst(t *testing.T) {
	th := newTestHost(t, nil)
	th.cfg.Tasks.RegisterRoutes(th.cfg.Router)
	now := time.Now().UTC().Truncate(time.Second)
	endedHostJob(t, th, jobA, now.Add(-2*time.Hour), "Older")
	endedHostJob(t, th, jobB, now.Add(-time.Hour), "Newer")
	th.start()
	sess := th.connect()

	history := callHistory(t, sess, map[string]any{})
	if want := []string{jobB, jobA}; !slices.Equal(historyIDs(history), want) || history.More {
		t.Fatalf("tasks.history = %v more=%v, want %v more=false", historyIDs(history), history.More, want)
	}
	// A record's request, which may hold secrets, never reaches the answer.
	raw := readResult(t, openStream(t, sess, "tasks.history", map[string]any{}, ""))
	if raw.Status != "ok" {
		t.Fatalf("tasks.history = %#v, want ok", raw)
	}
	if strings.Contains(string(raw.Data), "secret") {
		t.Fatalf("tasks.history = %s, want no record's request in it", raw.Data)
	}
	entry := history.Entries[0]
	if entry.Task.State != bridgeipc.TaskStateCompleted || entry.Task.Metadata == nil || entry.Task.Metadata.Label != "Newer" ||
		entry.Task.FinishedAt == nil || !entry.Task.FinishedAt.Equal(now.Add(-time.Hour)) ||
		entry.Task.StartedAt == nil || !entry.Task.StartedAt.Equal(now.Add(-time.Hour-time.Minute)) {
		t.Fatalf("entry = %+v, want a completed job labelled Newer with its times", entry)
	}
	if entry.Unit != "" || entry.Output || entry.Reason != "" {
		t.Fatalf("entry = %+v, want no unit, output or reason for a host job", entry)
	}
}

func TestTasksHistoryIncludesScheduledRunsOnTheRootHost(t *testing.T) {
	dir := t.TempDir()
	th := scheduledHost(t, dir, nil)
	now := time.Now().UTC().Truncate(time.Second)
	newest := endedScriptRun(t, th.cfg.Scheduled, invocationA, scriptUnit, now.Add(-time.Minute))
	endedHostJob(t, th, jobA, now.Add(-2*time.Minute), "Host job")
	oldest := endedScriptRun(t, th.cfg.Scheduled, invocationB, scriptUnit, now.Add(-3*time.Minute))
	if err := os.WriteFile(th.cfg.Scheduled.OutputPath(newest.ID), []byte("line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	th.start()

	history := callHistory(t, th.connect(), map[string]any{})
	if want := []string{newest.ID, jobA, oldest.ID}; !slices.Equal(historyIDs(history), want) {
		t.Fatalf("tasks.history = %v, want %v", historyIDs(history), want)
	}
	if got := history.Entries[0]; got.Unit != scriptUnit || !got.Output || got.Task.Metadata == nil || got.Task.Metadata.Label != "Nightly backup" {
		t.Fatalf("scheduled entry = %+v, want its unit, its output and its label", got)
	}
	if got := history.Entries[1]; got.Unit != "" || got.Output {
		t.Fatalf("host entry = %+v, want no unit and no output", got)
	}
	if got := history.Entries[2]; got.Unit != scriptUnit || got.Output {
		t.Fatalf("scheduled entry with no log = %+v, want its unit and no output", got)
	}
}

func TestTasksHistoryReportsNoOutputForALink(t *testing.T) {
	th := rootScheduledHost(t)
	run := endedScriptRun(t, th.cfg.Scheduled, invocationA, scriptUnit, time.Now().UTC())
	// tasks.output follows no link, so a link at the output's path is no output.
	target := filepath.Join(t.TempDir(), "elsewhere.log")
	if err := os.WriteFile(target, []byte("line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, th.cfg.Scheduled.OutputPath(run.ID)); err != nil {
		t.Fatal(err)
	}
	th.start()

	history := callHistory(t, th.connect(), map[string]any{})
	if len(history.Entries) != 1 || history.Entries[0].Task.ID != run.ID || history.Entries[0].Output {
		t.Fatalf("tasks.history = %+v, want the run with no output", history)
	}
}

func TestTasksHistoryOfAUnit(t *testing.T) {
	th := scheduledHost(t, t.TempDir(), nil)
	now := time.Now().UTC().Truncate(time.Second)
	wanted := endedScriptRun(t, th.cfg.Scheduled, invocationA, scriptUnit, now.Add(-time.Minute))
	endedScriptRun(t, th.cfg.Scheduled, invocationB, "linuxio-schedule-other.service", now.Add(-2*time.Minute))
	endedHostJob(t, th, jobA, now.Add(-3*time.Minute), "Host job")
	th.start()

	history := callHistory(t, th.connect(), map[string]any{"unit": scriptUnit})
	if want := []string{wanted.ID}; !slices.Equal(historyIDs(history), want) || history.More {
		t.Fatalf("tasks.history of a unit = %v more=%v, want %v more=false", historyIDs(history), history.More, want)
	}
}

func TestTasksHistoryCarriesTheReason(t *testing.T) {
	th := newTestHost(t, nil)
	th.cfg.Tasks.RegisterRoutes(th.cfg.Router)
	record := endedHostJob(t, th, jobA, time.Now().UTC(), "Interrupted")
	if _, err := th.cfg.Store.Update(context.Background(), record.ID, func(r *jobs.Record) error {
		r.State, r.Reason = jobs.StateInterrupted, jobs.ReasonReboot
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	th.start()

	history := callHistory(t, th.connect(), map[string]any{})
	if len(history.Entries) != 1 || history.Entries[0].Reason != "reboot" || history.Entries[0].Task.State != bridgeipc.TaskStateInterrupted {
		t.Fatalf("tasks.history = %+v, want one interrupted job with reason reboot", history)
	}
}

func TestTasksHistoryHidesRecordsOfOtherExecutors(t *testing.T) {
	th := scheduledHost(t, t.TempDir(), nil)
	now := time.Now().UTC()
	for store, id := range map[*jobs.Store]string{th.cfg.Store: jobB, th.cfg.Scheduled: jobC} {
		createRuns(t, store, jobs.Record{
			ID: id, Route: "network.apply", Owner: jobs.Owner{UID: th.cfg.UID, Username: "alice"},
			State: jobs.StateCompleted, CreatedAt: now, StartedAt: now, FinishedAt: now,
			Executor: jobs.Executor{Kind: "netplan", Unit: "/io/netplan"}, Protocol: jobs.Protocol,
		})
	}
	endedHostJob(t, th, jobA, now.Add(-time.Minute), "Host job")
	th.start()

	history := callHistory(t, th.connect(), map[string]any{})
	if want := []string{jobA}; !slices.Equal(historyIDs(history), want) {
		t.Fatalf("tasks.history = %v, want %v: a record no host ran is not listed", historyIDs(history), want)
	}
}

func TestUserHostHistoryHasNoScheduledRuns(t *testing.T) {
	th := newTestHost(t, nil)
	th.cfg.Tasks.RegisterRoutes(th.cfg.Router)
	now := time.Now().UTC().Truncate(time.Second)
	// The record of a unit's run in the host's own store is no job it ran.
	endedScriptRun(t, th.cfg.Store, invocationA, scriptUnit, now.Add(-time.Minute))
	endedHostJob(t, th, jobA, now.Add(-2*time.Minute), "Host job")
	th.start()
	sess := th.connect()

	history := callHistory(t, sess, map[string]any{})
	if want := []string{jobA}; !slices.Equal(historyIDs(history), want) {
		t.Fatalf("tasks.history = %v, want only %v", historyIDs(history), want)
	}
	history = callHistory(t, sess, map[string]any{"unit": scriptUnit})
	if len(history.Entries) != 0 || history.More {
		t.Fatalf("tasks.history of a unit = %+v, want none", history)
	}
}

func TestTasksHistoryMergesTheStoresThroughEqualTimes(t *testing.T) {
	th := scheduledHost(t, t.TempDir(), nil)
	now := time.Now().UTC().Truncate(time.Second)
	tie := now.Add(-2 * time.Minute)
	endedHostJob(t, th, jobA, now, "Newest")
	endedHostJob(t, th, jobB, tie, "Tied")
	run := endedScriptRun(t, th.cfg.Scheduled, invocationA, scriptUnit, tie)
	endedScriptRun(t, th.cfg.Scheduled, invocationB, scriptUnit, now.Add(-3*time.Minute))
	th.start()
	sess := th.connect()

	first := callHistory(t, sess, map[string]any{"limit": 2})
	if want := []string{jobA, jobB, run.ID}; !slices.Equal(historyIDs(first), want) || !first.More {
		t.Fatalf("first page = %v more=%v, want %v more=true", historyIDs(first), first.More, want)
	}
	second := callHistory(t, sess, map[string]any{"limit": 2, "before": tie})
	if len(second.Entries) != 1 || second.More {
		t.Fatalf("second page = %v more=%v, want the one older run", historyIDs(second), second.More)
	}
}

func TestTasksHistoryEntriesLeaveOutWhatThePageDoesNotShow(t *testing.T) {
	th := scheduledHost(t, t.TempDir(), nil)
	now := time.Now().UTC().Truncate(time.Second)
	completed := endedHostJob(t, th, jobA, now, "2 items")
	failed := endedHostJob(t, th, jobB, now.Add(-time.Minute), "Update")
	message := strings.Repeat("é", 3000) // 6000 bytes
	for id, update := range map[string]func(r *jobs.Record){
		completed.ID: func(r *jobs.Record) {
			r.Target, r.Result = []string{"/srv/a", "/srv/b"}, jsontext.Value(`{"ok":true}`)
			r.Metadata = jsontext.Value(`{"label":"2 items","action":"delete","path":"/srv","identity":["/srv/a","/srv/b"],"packageIds":["pkg;1"]}`)
			r.Progress = jsontext.Value(`{"percentage":100,"message":"Deleted"}`)
		},
		failed.ID: func(r *jobs.Record) {
			r.State, r.Error = jobs.StateFailed, &jobs.Error{Code: 500, Message: message}
		},
	} {
		if _, err := th.cfg.Store.Update(context.Background(), id, func(r *jobs.Record) error {
			update(r)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	run := endedScriptRun(t, th.cfg.Scheduled, invocationA, scriptUnit, now.Add(-2*time.Minute))
	th.start()

	history := callHistory(t, th.connect(), map[string]any{})
	if want := []string{completed.ID, failed.ID, run.ID}; !slices.Equal(historyIDs(history), want) {
		t.Fatalf("tasks.history = %v, want %v", historyIDs(history), want)
	}
	got := history.Entries[0].Task
	if got.Result != nil || got.Progress != nil {
		t.Fatalf("host entry = %+v, want no result and no progress", got)
	}
	if got.Metadata == nil || got.Metadata.Label != "2 items" || got.Metadata.Action != "delete" || got.Metadata.Path != "/srv" ||
		got.Metadata.Identity != nil || got.Metadata.PackageIDs != nil {
		t.Fatalf("host entry metadata = %+v, want its label, action and path and no identity or package IDs", got.Metadata)
	}
	errMessage := history.Entries[1].Task.Error
	if errMessage == nil || !strings.HasSuffix(errMessage.Message, "…") || !utf8.ValidString(errMessage.Message) ||
		len(strings.TrimSuffix(errMessage.Message, "…")) > 4096 || !strings.HasPrefix(message, strings.TrimSuffix(errMessage.Message, "…")) {
		t.Fatalf("failed entry error = %d bytes, want the message cut to at most 4096 bytes on a character boundary and marked", len(errMessage.Message))
	}
	if got := history.Entries[2].Task.Metadata; got == nil || !slices.Equal(got.Identity, []string{testScheduleID}) {
		t.Fatalf("scheduled entry metadata = %+v, want its identity [%s]", got, testScheduleID)
	}
}

func TestHistoryPageOfLargeRecordsFitsARelayFrame(t *testing.T) {
	const maxRelayFrame = 16 << 20
	identity := make([]string, 0, 1000)
	for i := range cap(identity) {
		identity = append(identity, fmt.Sprintf("/srv/data/a/long/folder/name/file-%05d.bin", i))
	}
	metadata, err := json.Marshal(bridgeipc.TaskMetadata{Label: "1000 items", Action: "copy", Identity: identity, PackageIDs: identity[:500]})
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata) < 60<<10 {
		t.Fatalf("metadata = %d bytes, want at least 60 KiB", len(metadata))
	}
	now := time.Now().UTC()
	history := bridgeipc.TaskHistory{}
	for i := range 100 {
		record := jobs.Record{
			ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i), Route: "test.job", Owner: jobs.Owner{Username: "alice"},
			State: jobs.StateFailed, CreatedAt: now, StartedAt: now, FinishedAt: now, Target: identity,
			Metadata: metadata, Error: &jobs.Error{Code: 500, Message: strings.Repeat("x", 1<<20)},
			Progress: jsontext.Value(`{"percentage":50,"message":"` + strings.Repeat("y", 60<<10) + `"}`),
			Executor: jobs.Executor{Kind: "host"}, Protocol: jobs.Protocol,
		}
		history.Entries = append(history.Entries, bridgeipc.TaskHistoryEntry{Task: historySnapshot(record)})
	}
	data, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) >= maxRelayFrame {
		t.Fatalf("history page = %d bytes, want under the %d of a relay frame", len(data), maxRelayFrame)
	}
}

// rootScheduledHost is a scheduledHost that runs as root, so the sessions of
// its privileged bridges are privileged, as tasks.output asks.
func rootScheduledHost(t *testing.T) *testHost {
	t.Helper()
	dir := t.TempDir()
	th := newTestHost(t, func(c *Config) {
		c.UID, c.GID = 0, 0
		c.Peer = func(*net.UnixConn) (peercred.Cred, error) { return peercred.Cred{UID: 0}, nil }
		c.Scheduled = jobs.NewStore(filepath.Join(dir, "state"), filepath.Join(dir, "run"))
		c.UnitActive = func(context.Context, string, string) (bool, error) { return true, nil }
	})
	th.cfg.Tasks.RegisterRoutes(th.cfg.Router)
	return th
}

// writeRunOutput saves output as the output of the job id in store.
func writeRunOutput(t *testing.T, store *jobs.Store, id, output string) {
	t.Helper()
	if err := os.WriteFile(store.OutputPath(id), []byte(output), 0o600); err != nil {
		t.Fatal(err)
	}
}

func callOutput(t *testing.T, sess *relay.YamuxSession, request any) relay.ResultFrame {
	t.Helper()
	return readResult(t, openStream(t, sess, "tasks.output", request, ""))
}

func decodeOutput(t *testing.T, result relay.ResultFrame) bridgeipc.TaskOutput {
	t.Helper()
	var output bridgeipc.TaskOutput
	if result.Status != "ok" {
		t.Fatalf("tasks.output = %#v, want ok", result)
	}
	if err := json.Unmarshal(result.Data, &output); err != nil {
		t.Fatalf("json.Unmarshal(output %s): %v", result.Data, err)
	}
	return output
}

func TestTasksOutputReturnsAChunk(t *testing.T) {
	th := rootScheduledHost(t)
	run := endedScriptRun(t, th.cfg.Scheduled, invocationA, scriptUnit, time.Now().UTC())
	writeRunOutput(t, th.cfg.Scheduled, run.ID, "line one\nline two\n")
	th.start()
	sess := th.connect()

	got := decodeOutput(t, callOutput(t, sess, map[string]any{"taskId": run.ID, "offset": 5, "limit": 3}))
	if want := (bridgeipc.TaskOutput{Content: "one", Offset: 5, Size: 18}); got != want {
		t.Fatalf("tasks.output from 5 = %+v, want %+v", got, want)
	}
	// Without an offset it answers the end.
	got = decodeOutput(t, callOutput(t, sess, map[string]any{"taskId": run.ID, "limit": 9}))
	if want := (bridgeipc.TaskOutput{Content: "line two\n", Offset: 9, Size: 18}); got != want {
		t.Fatalf("tasks.output of the end = %+v, want %+v", got, want)
	}
}

func TestTasksOutputFromAnExplicitZeroOffset(t *testing.T) {
	th := rootScheduledHost(t)
	run := endedScriptRun(t, th.cfg.Scheduled, invocationA, scriptUnit, time.Now().UTC())
	writeRunOutput(t, th.cfg.Scheduled, run.ID, "line one\nline two\n")
	th.start()

	// An offset of 0 is the start, not the end an absent offset means.
	got := decodeOutput(t, callOutput(t, th.connect(), map[string]any{"taskId": run.ID, "offset": 0, "limit": 3}))
	if want := (bridgeipc.TaskOutput{Content: "lin", Offset: 0, Size: 18}); got != want {
		t.Fatalf("tasks.output from 0 = %+v, want %+v", got, want)
	}
}

func TestTasksOutputOfAnEmptyOutput(t *testing.T) {
	th := rootScheduledHost(t)
	run := endedScriptRun(t, th.cfg.Scheduled, invocationA, scriptUnit, time.Now().UTC())
	writeRunOutput(t, th.cfg.Scheduled, run.ID, "")
	th.start()
	sess := th.connect()

	for _, request := range []map[string]any{{"taskId": run.ID}, {"taskId": run.ID, "offset": 0}} {
		got := decodeOutput(t, callOutput(t, sess, request))
		if want := (bridgeipc.TaskOutput{Content: "", Offset: 0, Size: 0}); got != want {
			t.Fatalf("tasks.output %v of an empty output = %+v, want %+v", request, got, want)
		}
	}
}

func TestRootHostRefusesOutputForAnUnprivilegedSession(t *testing.T) {
	th := rootScheduledHost(t)
	run := endedScriptRun(t, th.cfg.Scheduled, invocationA, scriptUnit, time.Now().UTC())
	writeRunOutput(t, th.cfg.Scheduled, run.ID, "line\n")
	th.start()
	// A bridge of a session without sudo says so in its hello.
	sess := th.dial()
	stream := openStream(t, sess, bridgeipc.HelloRoute, bridgeipc.Hello{
		Protocol: jobs.Protocol,
		Session:  bridgeipc.HelloSession{Username: "alice", UID: 4242, GID: 4243, SessionRef: "ref-1"},
	}, "")
	if result := readResult(t, stream); result.Status != "ok" {
		t.Fatalf("hello result = %#v, want ok", result)
	}

	if result := callOutput(t, sess, map[string]any{"taskId": run.ID}); result.Code != 403 {
		t.Fatalf("tasks.output for an unprivileged session on the root host = %#v, want 403", result)
	}
}

func TestReadOutputReplacesInvalidUTF8(t *testing.T) {
	th := rootScheduledHost(t)
	run := endedScriptRun(t, th.cfg.Scheduled, invocationA, scriptUnit, time.Now().UTC())
	writeRunOutput(t, th.cfg.Scheduled, run.ID, "a\xffb\x1b[31m")
	th.start()

	got := decodeOutput(t, callOutput(t, th.connect(), map[string]any{"taskId": run.ID}))
	if want := (bridgeipc.TaskOutput{Content: "a�b\x1b[31m", Offset: 0, Size: 8}); got != want {
		t.Fatalf("tasks.output = %#v, want %#v: invalid bytes replaced, control characters kept", got, want)
	}
}

func TestTasksOutputStartsAtACharacterBoundary(t *testing.T) {
	th := rootScheduledHost(t)
	run := endedScriptRun(t, th.cfg.Scheduled, invocationA, scriptUnit, time.Now().UTC())
	writeRunOutput(t, th.cfg.Scheduled, run.ID, "x\xc3\xa9y")
	th.start()
	sess := th.connect()

	got := decodeOutput(t, callOutput(t, sess, map[string]any{"taskId": run.ID, "offset": 2, "limit": 2}))
	if want := (bridgeipc.TaskOutput{Content: "y", Offset: 3, Size: 4}); got != want {
		t.Fatalf("tasks.output from inside é = %#v, want %#v: the rest of é skipped", got, want)
	}
	got = decodeOutput(t, callOutput(t, sess, map[string]any{"taskId": run.ID, "offset": 1, "limit": 3}))
	if want := (bridgeipc.TaskOutput{Content: "éy", Offset: 1, Size: 4}); got != want {
		t.Fatalf("tasks.output from é = %#v, want %#v", got, want)
	}
}

func TestTasksOutputOfAJobWithoutOneIs404(t *testing.T) {
	th := rootScheduledHost(t)
	run := endedScriptRun(t, th.cfg.Scheduled, invocationA, scriptUnit, time.Now().UTC())
	// A job of the host's own saves no output: a file where one would be is
	// not read.
	own := endedHostJob(t, th, jobA, time.Now().UTC(), "Host job")
	writeRunOutput(t, th.cfg.Store, own.ID, "own\n")
	th.start()
	sess := th.connect()

	for _, id := range []string{run.ID, own.ID, jobB} {
		if result := callOutput(t, sess, map[string]any{"taskId": id}); result.Code != 404 {
			t.Fatalf("tasks.output of %s = %#v, want 404", id, result)
		}
	}
}

func TestUserHostAnswersNoOutput(t *testing.T) {
	th := newTestHost(t, asUserHost)
	th.cfg.Tasks.RegisterRoutes(th.cfg.Router)
	own := endedHostJob(t, th, jobA, time.Now().UTC(), "Host job")
	writeRunOutput(t, th.cfg.Store, own.ID, "own\n")
	th.start()

	stored := storedJobs{th.host}
	for _, id := range []string{own.ID, jobB} {
		if _, err := stored.Output(context.Background(), bridgeipc.TaskOutputRequest{TaskID: id}); bridgeipc.StatusCode(err) != 404 {
			t.Fatalf("Output(%s) on a user host = %v, want 404", id, err)
		}
	}
	if stored.hasOutput(own.ID) {
		t.Fatal("a user host reports a saved output")
	}
	// Its sessions are never privileged, so its task service refuses first.
	if result := callOutput(t, th.connect(), map[string]any{"taskId": own.ID}); result.Code != 403 {
		t.Fatalf("tasks.output on a user host = %#v, want 403", result)
	}
}
