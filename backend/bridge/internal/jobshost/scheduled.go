package jobshost

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

// scheduledPoll is how often tasks.events reads the progress of the scheduled
// runs it follows; a variable so tests can shorten it.
var scheduledPoll = 2 * time.Second

const (
	// scheduledRelist is the longest tasks.events goes without listing the
	// scheduled runs. A run whose unit ended without its end hook leaves the
	// store as it was; only a listing asks systemd about it.
	scheduledRelist = 30 * time.Second
	// racyModTime is how recent a ModTime may be before a listing that read
	// it lists again at the next poll: a record written in the same clock
	// tick as the read leaves the time as it was.
	racyModTime = time.Second
)

// scheduledRunning reports whether r is a scheduled run that runs.
func scheduledRunning(r jobs.Record) bool {
	return r.State == jobs.StateRunning && r.Executor.Kind == jobs.ExecutorUnit
}

// scheduledRuns checks the scheduled runs' records with systemd.
func (h *Host) scheduledRuns() scheduledrun.Liveness {
	return scheduledrun.Liveness{
		Store:  h.cfg.Scheduled,
		BootID: h.executor.BootID,
		Active: h.cfg.UnitActive,
		Alert:  h.cfg.Alert,
		Now:    h.cfg.Now,
	}
}

// scheduledRecord is the record of the scheduled run id. A running one is
// checked with systemd first, as a listed one is.
func (s storedJobs) scheduledRecord(ctx context.Context, id string) (jobs.Record, error) {
	record, err := s.h.cfg.Scheduled.Get(ctx, id)
	if err == nil && record.State == jobs.StateRunning {
		if _, err = s.h.scheduledRuns().Running(ctx); err != nil {
			return jobs.Record{}, fmt.Errorf("check scheduled run %s: %w", id, err)
		}
		record, err = s.h.cfg.Scheduled.Get(ctx, id)
	}
	if errors.Is(err, jobs.ErrNotFound) {
		return record, bridgeipc.NewError(fmt.Sprintf("task not found: %s", id), 404)
	}
	if err != nil {
		return record, fmt.Errorf("read scheduled run %s: %w", id, err)
	}
	return record, nil
}

// snapshot is the task snapshot of a stored job; a running scheduled run's
// carries the progress its run last wrote.
func (s storedJobs) snapshot(record jobs.Record) bridgeipc.TaskSnapshot {
	if scheduledRunning(record) {
		return RunningSnapshot(record, s.progress(record.ID))
	}
	return snapshotFromRecord(record)
}

// progress is the progress file of the scheduled run id, nil when there is
// none.
func (s storedJobs) progress(id string) jsontext.Value {
	progress, err := s.h.cfg.Scheduled.ReadProgress(id)
	if err != nil {
		if !errors.Is(err, jobs.ErrNotFound) {
			slog.Debug("scheduled run progress unreadable", "job_id", id, "error", err)
		}
		return nil
	}
	return progress
}

// List returns the scheduled runs that run. A job of the host's own with no
// task has ended, and history is listed elsewhere.
func (s storedJobs) List(ctx context.Context) ([]bridgeipc.TaskSnapshot, error) {
	if s.h.cfg.Scheduled == nil {
		return nil, nil
	}
	records, err := s.h.scheduledRuns().Running(ctx)
	if err != nil {
		return nil, err
	}
	snapshots := make([]bridgeipc.TaskSnapshot, 0, len(records))
	for _, record := range records {
		snapshots = append(snapshots, s.snapshot(record))
	}
	return snapshots, nil
}

// History lists the ended jobs of the host, newest first: its own and, on the
// root host, the scheduled runs. With a unit it lists the runs of that unit
// only, and a host with no scheduled store has none. Each store is asked for a
// page of its own, so the merged page, cut at the limit through records that
// ended at the same instant, holds the newest records of both.
func (s storedJobs) History(ctx context.Context, req bridgeipc.TaskHistoryRequest) (bridgeipc.TaskHistory, error) {
	query := jobs.HistoryQuery{Limit: req.Limit}
	if req.Before != nil {
		query.Before = *req.Before
	}
	var lists [][]jobs.Record
	more := false
	if req.Unit == "" {
		own := query
		own.Executor = "host"
		records, ownMore, err := s.h.cfg.Store.History(ctx, own)
		if err != nil {
			return bridgeipc.TaskHistory{}, fmt.Errorf("list ended jobs: %w", err)
		}
		lists, more = append(lists, records), ownMore
	}
	if s.h.cfg.Scheduled != nil {
		runs := query
		runs.Executor, runs.Target = jobs.ExecutorUnit, req.Unit
		records, runsMore, err := s.h.cfg.Scheduled.History(ctx, runs)
		if err != nil {
			return bridgeipc.TaskHistory{}, fmt.Errorf("list ended scheduled runs: %w", err)
		}
		lists, more = append(lists, records), more || runsMore
	}
	page, cut := jobs.MergeHistory(query, lists...)
	history := bridgeipc.TaskHistory{Entries: make([]bridgeipc.TaskHistoryEntry, 0, len(page)), More: more || cut}
	for _, record := range page {
		entry := bridgeipc.TaskHistoryEntry{Task: historySnapshot(record), Reason: string(record.Reason)}
		if record.Executor.Kind == jobs.ExecutorUnit {
			entry.Unit, entry.Output = record.Executor.Unit, s.hasOutput(record.ID)
		}
		history.Entries = append(history.Entries, entry)
	}
	return history, nil
}

// maxHistoryErrorBytes bounds the error message of a history entry, so a page
// of entries fits a relay frame.
const maxHistoryErrorBytes = 4096

// historySnapshot is the snapshot of an ended job as a history entry shows
// it: no result or last progress, which a record can hold up to its size
// limit, no paths or packages of a host job, and an error message cut to
// maxHistoryErrorBytes on a character boundary. tasks.get answers the whole.
func historySnapshot(record jobs.Record) bridgeipc.TaskSnapshot {
	snapshot := snapshotFromRecord(record)
	snapshot.Result, snapshot.Progress = nil, nil
	withoutTargets(record, &snapshot)
	if snapshot.Error != nil && len(snapshot.Error.Message) > maxHistoryErrorBytes {
		cut := maxHistoryErrorBytes
		for cut > 0 && !utf8.RuneStart(snapshot.Error.Message[cut]) {
			cut--
		}
		snapshot.Error.Message = snapshot.Error.Message[:cut] + "…"
	}
	return snapshot
}

// hasOutput reports whether a saved output of the scheduled run id exists: a
// regular file, as Output reads, not a link to one. A host with no scheduled
// store has none.
func (s storedJobs) hasOutput(id string) bool {
	if s.h.cfg.Scheduled == nil {
		return false
	}
	info, err := os.Lstat(s.h.cfg.Scheduled.OutputPath(id))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Debug("scheduled run output not checked", "job_id", id, "error", err)
	}
	return err == nil && info.Mode().IsRegular()
}

// Output reads a chunk of the saved output of the scheduled run id. Only the
// scheduled store keeps outputs: a job of the host's own saves none, and a
// host with no scheduled store, a user's, has none to read. A chunk that
// starts inside a character skips the rest of it and reports where it starts,
// so it starts on a character boundary; one whose requested end falls inside
// a character ends in U+FFFD, which a viewer avoids by ending each earlier
// chunk at the offset the next chunk reports. Invalid bytes become U+FFFD.
func (s storedJobs) Output(_ context.Context, req bridgeipc.TaskOutputRequest) (bridgeipc.TaskOutput, error) {
	if s.h.cfg.Scheduled == nil {
		return bridgeipc.TaskOutput{}, fmt.Errorf("%w: task %s has no saved output", bridgeipc.ErrNotFound, req.TaskID)
	}
	offset := int64(-1)
	if req.Offset != nil {
		offset = *req.Offset
	}
	data, start, size, err := s.h.cfg.Scheduled.ReadOutput(req.TaskID, offset, req.Limit)
	if errors.Is(err, jobs.ErrNotFound) {
		return bridgeipc.TaskOutput{}, fmt.Errorf("%w: task %s has no saved output", bridgeipc.ErrNotFound, req.TaskID)
	}
	if err != nil {
		return bridgeipc.TaskOutput{}, fmt.Errorf("read output of scheduled run %s: %w", req.TaskID, err)
	}
	if start > 0 {
		skip := 0
		for skip < len(data) && skip < utf8.UTFMax-1 && !utf8.RuneStart(data[skip]) {
			skip++
		}
		data, start = data[skip:], start+int64(skip)
	}
	return bridgeipc.TaskOutput{Content: strings.ToValidUTF8(string(data), "�"), Offset: start, Size: size}, nil
}

// Events follows the scheduled runs until ctx ends: a snapshot of each run
// that runs, when the stream opens or the run begins, a progress event when
// its progress file changes and a terminal event when it ends. It writes
// nothing once it returns. A job of the host's own with no task sends no more
// events.
func (s storedJobs) Events(ctx context.Context, stream net.Conn) error {
	if s.h.cfg.Scheduled == nil {
		return nil
	}
	f := &scheduledEvents{s: s, stream: stream, runs: make(map[string]jobs.Record)}
	ticker := time.NewTicker(scheduledPoll)
	defer ticker.Stop()
	for f.poll(ctx) {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
	return nil
}

// scheduledEvents is what one tasks.events stream was sent of the scheduled
// runs.
type scheduledEvents struct {
	s      storedJobs
	stream net.Conn
	// runs holds the runs the stream was sent running, each with the progress
	// it was last sent in Progress.
	runs map[string]jobs.Record
	// listed is when the store was last listed and modTime its ModTime then;
	// racy says that ModTime was too recent to prove nothing changed since.
	listed  time.Time
	modTime time.Time
	racy    bool
}

// poll sends what changed since the last poll. It reports false once the
// stream takes no more events.
func (f *scheduledEvents) poll(ctx context.Context) bool {
	if f.due() && !f.relist(ctx) {
		return false
	}
	return f.sendProgress(ctx)
}

// due reports whether to list the store again: at the first poll, after a
// record was created, replaced or removed, and every scheduledRelist.
// Between listings a poll reads only the progress files of the runs it knows.
func (f *scheduledEvents) due() bool {
	modTime, err := f.s.h.cfg.Scheduled.ModTime()
	if err != nil {
		slog.Debug("scheduled run store not checked for changes", "error", err)
	}
	return f.dueAt(time.Now(), modTime, err == nil)
}

// dueAt is due's decision at now, given the store's ModTime when known.
func (f *scheduledEvents) dueAt(now, modTime time.Time, known bool) bool {
	changed := known && !modTime.Equal(f.modTime)
	if !f.listed.IsZero() && !f.racy && !changed && now.Sub(f.listed) < scheduledRelist {
		return false
	}
	// A ModTime ahead of the clock, after the clock was set back, is not young.
	age := now.Sub(modTime)
	f.listed, f.modTime, f.racy = now, modTime, age >= 0 && age < racyModTime
	return true
}

// relist sends a snapshot of each run that runs and the stream was not sent,
// and the terminal event of each run it was sent that no longer runs.
func (f *scheduledEvents) relist(ctx context.Context) bool {
	records, err := f.s.h.scheduledRuns().Running(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("scheduled runs not listed for task events", "error", err)
		}
		f.listed = time.Time{} // the next poll lists again
		return true
	}
	running := make(map[string]bool, len(records))
	for _, record := range records {
		running[record.ID] = true
		if _, ok := f.runs[record.ID]; ok {
			continue
		}
		record.Progress = f.s.progress(record.ID)
		if !f.send(ctx, bridgeipc.TaskEvent{Type: bridgeipc.TaskEventSnapshot, Task: snapshotFromRecord(record)}) {
			return false
		}
		f.runs[record.ID] = record
	}
	for id, run := range f.runs {
		if running[id] {
			continue
		}
		delete(f.runs, id)
		if !f.send(ctx, f.ended(ctx, run)) {
			return false
		}
	}
	return true
}

// sendProgress sends a progress event for each run the stream follows whose
// progress file changed.
func (f *scheduledEvents) sendProgress(ctx context.Context) bool {
	for id, run := range f.runs {
		progress := f.s.progress(id)
		if len(progress) == 0 || bytes.Equal(progress, run.Progress) {
			continue
		}
		run.Progress = progress
		f.runs[id] = run
		snapshot := snapshotFromRecord(run)
		if snapshot.Progress == nil {
			continue // not decoded, as snapshotFromRecord logged
		}
		if !f.send(ctx, bridgeipc.TaskEvent{Type: bridgeipc.TaskEventProgress, Task: snapshot, Progress: *snapshot.Progress}) {
			return false
		}
	}
	return true
}

// ended is the terminal event of a run the stream followed that no longer
// runs, built from its record. A run whose record is gone, or still says
// running when systemd says it does not, is reported interrupted, so the
// client drops it.
func (f *scheduledEvents) ended(ctx context.Context, run jobs.Record) bridgeipc.TaskEvent {
	record, err := f.s.h.cfg.Scheduled.Get(ctx, run.ID)
	if err != nil && !errors.Is(err, jobs.ErrNotFound) && ctx.Err() == nil {
		slog.Debug("ended scheduled run unreadable", "job_id", run.ID, "error", err)
	}
	if err != nil || record.State == jobs.StateRunning {
		record = run
		record.State, record.FinishedAt = jobs.StateInterrupted, f.s.h.cfg.Now().UTC()
	}
	record.Progress = run.Progress
	return terminalEvent(snapshotFromRecord(record))
}

// send writes event unless ctx has ended: once its caller stops Events, it
// writes nothing more.
func (f *scheduledEvents) send(ctx context.Context, event bridgeipc.TaskEvent) bool {
	return ctx.Err() == nil && bridgeipc.WriteTaskEvent(f.stream, event)
}

// terminalEvent is the event the task registry sends when a task ends as
// snapshot did.
func terminalEvent(snapshot bridgeipc.TaskSnapshot) bridgeipc.TaskEvent {
	switch snapshot.State {
	case bridgeipc.TaskStateCompleted:
		return bridgeipc.TaskEvent{Type: bridgeipc.TaskEventResult, Task: snapshot, Result: snapshot.Result}
	case bridgeipc.TaskStateCanceled:
		return bridgeipc.TaskEvent{Type: bridgeipc.TaskEventCanceled, Task: snapshot, Error: snapshot.Error}
	default: // failed or interrupted
		return bridgeipc.TaskEvent{Type: bridgeipc.TaskEventError, Task: snapshot, Error: snapshot.Error}
	}
}
