package jobshost

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
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
