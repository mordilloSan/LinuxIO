package jobshost

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
)

const (
	// progressInterval is the least time between two progress writes of a job.
	progressInterval = 500 * time.Millisecond
	// storeTimeout bounds one recorder store operation.
	storeTimeout = 15 * time.Second
)

// recorder is the bridgeipc.Recorder of the host: it keeps one store record
// per Mutation job and its latest progress.
type recorder struct {
	h *Host

	mu sync.Mutex
	// stopped: the host has shut down; progress is no longer timed, and
	// Finished flushes the latest.
	stopped bool
	// interrupting: the shutdown has listed the running jobs to interrupt;
	// Started interrupts a job admitted after that.
	interrupting bool
	// jobs holds the started jobs whose record is not finished. A job that
	// ended stays until Finished has finished its record, so a shutdown
	// waits for that write; its progress is ignored from the end on.
	jobs map[string]*job
}

type job struct {
	task *bridgeipc.Task
	// created: Started has written the record. finished: the task ended, and
	// Finished is finishing the record, or left that to Started when it came
	// first. resumedFrom: Started reopened the interrupted record of a
	// resumed job, interrupted for that reason; ReasonNone for any other job.
	created     bool
	finished    bool
	resumedFrom jobs.Reason
	pending     jsontext.Value
	lastWrite   time.Time
	timer       *time.Timer
}

// running counts the jobs whose record is not finished, an ended one until
// Finished has written its end.
func (r *recorder) running() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.jobs)
}

// interruptAll lists the running jobs for the shutdown to interrupt and the
// ended ones, whose record Finished is finishing or about to, for it to wait
// for, and makes Started interrupt every job that starts from now on.
func (r *recorder) interruptAll() (running, ending []*bridgeipc.Task) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interrupting = true
	for _, j := range r.jobs {
		if j.finished || j.task.IsTerminal() {
			ending = append(ending, j.task)
		} else {
			running = append(running, j.task)
		}
	}
	return running, ending
}

// Started records a job before it runs, or moves the interrupted record of a
// resumed job back to running. The record is finished here when
// Finished came first, or never will: a task canceled while queued can turn
// terminal before it had this recorder.
func (r *recorder) Started(task *bridgeipc.Task, raw jsontext.Value) {
	id := task.ID()
	j := &job{task: task}
	r.mu.Lock()
	r.jobs[id] = j
	late := r.interrupting
	r.mu.Unlock()

	ctx, cancel := jobs.DetachedContext(storeTimeout)
	defer cancel()
	err := r.h.cfg.Store.Create(ctx, r.h.newRecord(task, raw))
	resumedFrom := jobs.ReasonNone
	switch {
	case errors.Is(err, jobs.ErrExists):
		// A resumed job keeps its record.
		if resumedFrom, err = r.h.rerun(ctx, id); err != nil {
			slog.Error("job record not reopened for a rerun", "job_id", id, "route", task.Type(), "error", err)
		}
	case err != nil:
		slog.Error("job record not created", "job_id", id, "route", task.Type(), "error", err)
	}

	r.mu.Lock()
	j.created, j.resumedFrom = true, resumedFrom
	finish := j.finished || task.IsTerminal()
	if finish && r.jobs[id] == j {
		delete(r.jobs, id)
	}
	r.mu.Unlock()
	switch {
	case finish:
		r.finish(ctx, task, resumedFrom)
	case late:
		// Admitted as the host began to stop, after the shutdown listed the
		// jobs it interrupts.
		r.h.interrupt(ctx, task)
	}
}

func (h *Host) newRecord(task *bridgeipc.Task, raw jsontext.Value) jobs.Record {
	now := h.cfg.Now().UTC()
	owner := task.Owner()
	route, _ := h.cfg.Router.Route(task.Type())
	record := jobs.Record{
		ID:        task.ID(),
		Route:     task.Type(),
		Owner:     h.recordOwner(owner),
		State:     jobs.StateRunning,
		CreatedAt: now,
		StartedAt: now,
		Request:   raw,
		Executor:  h.executor,
		Resumable: route.Resume != nil,
		Protocol:  jobs.Protocol,
	}
	if metadata := task.Snapshot().Metadata; metadata != nil {
		// The identity is the target, which a snapshot of the record shows
		// as its identity; stored once, a batch of thousands of paths still
		// fits a record.
		record.Target = metadata.Identity
		stored := *metadata
		stored.Identity = nil
		data, err := json.Marshal(stored)
		if err != nil {
			slog.Debug("job metadata not encoded", "job_id", task.ID(), "error", err)
		}
		record.Metadata = data
	}
	return record
}

// Progress writes a job's progress at most once per progressInterval; the
// latest value of an interval is written when it ends.
func (r *recorder) Progress(task *bridgeipc.Task, p bridgeipc.TaskProgress) {
	data, err := json.Marshal(p)
	if err != nil {
		slog.Debug("job progress not encoded", "job_id", task.ID(), "error", err)
		return
	}
	id := task.ID()
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok || j.finished {
		return
	}
	j.pending = data
	if j.timer != nil || r.stopped {
		return
	}
	if wait := progressInterval - time.Since(j.lastWrite); wait > 0 {
		j.timer = time.AfterFunc(wait, func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			j.timer = nil
			if r.jobs[id] == j {
				r.writeProgressLocked(id, j)
			}
		})
		return
	}
	r.writeProgressLocked(id, j)
}

func (r *recorder) writeProgressLocked(id string, j *job) {
	if j.pending == nil {
		return
	}
	// A transient job's record may already be gone; its progress no longer matters.
	if err := r.h.cfg.Store.WriteProgress(id, j.pending); err != nil && !errors.Is(err, jobs.ErrNotFound) {
		slog.Warn("job progress not written", "job_id", id, "error", err)
	}
	j.pending, j.lastWrite = nil, time.Now()
}

// stop cancels the pending progress timers when the host shuts down and
// writes their progress now.
func (r *recorder) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = true
	for id, j := range r.jobs {
		if j.timer != nil {
			j.timer.Stop()
			j.timer = nil
			r.writeProgressLocked(id, j)
		}
	}
}

// Finished flushes a job's last progress and finishes its record, unless
// Started has not written it yet, and only then drops the job. It runs on the
// goroutine that ended the task, usually the task's own, which the host waits
// for only through this list, so a shutdown (interruptAll) waits for the
// write. A job Started finishes is dropped at once: Started runs on the serve
// goroutine or a connection's, which the host waits for.
func (r *recorder) Finished(task *bridgeipc.Task) {
	id := task.ID()
	r.mu.Lock()
	j, ok := r.jobs[id]
	if !ok {
		r.mu.Unlock()
		return
	}
	if j.timer != nil {
		j.timer.Stop()
		j.timer = nil
	}
	r.writeProgressLocked(id, j)
	j.finished = true
	created, resumedFrom := j.created, j.resumedFrom
	if !created {
		delete(r.jobs, id)
	}
	r.mu.Unlock()
	if !created {
		return
	}
	ctx, cancel := jobs.DetachedContext(storeTimeout)
	defer cancel()
	r.finish(ctx, task, resumedFrom)
	r.mu.Lock()
	if r.jobs[id] == j {
		delete(r.jobs, id)
	}
	r.mu.Unlock()
}

// CancelRequested marks the running record of a job its owner canceled, before
// the handler is told to stop, so neither a shutdown nor a later boot resumes it.
func (r *recorder) CancelRequested(task *bridgeipc.Task) {
	id := task.ID()
	r.mu.Lock()
	j, ok := r.jobs[id]
	created := ok && j.created && !j.finished
	r.mu.Unlock()
	if !created {
		return
	}
	ctx, cancel := jobs.DetachedContext(storeTimeout)
	defer cancel()
	_, err := r.h.cfg.Store.Update(ctx, id, func(record *jobs.Record) error {
		if record.State == jobs.StateRunning {
			record.CancelRequested = true
		}
		return nil
	})
	// A job that completed in under a second meanwhile has no record left.
	if err != nil && !errors.Is(err, jobs.ErrNotFound) {
		slog.Warn("job cancel request not recorded", "job_id", id, "error", err)
	}
}

// Checkpoint stores the checkpoint a running job saved in its record, where a
// pick-up after its host died finds it. The store fsyncs a changed
// checkpoint. A job whose record is no longer running, such as one its host's
// shutdown interrupted, saves nothing.
func (r *recorder) Checkpoint(task *bridgeipc.Task, checkpoint jsontext.Value) error {
	ctx, cancel := jobs.DetachedContext(storeTimeout)
	defer cancel()
	_, err := r.h.cfg.Store.Update(ctx, task.ID(), func(record *jobs.Record) error {
		if record.State != jobs.StateRunning {
			return fmt.Errorf("job is %s", record.State)
		}
		record.Checkpoint = checkpoint
		return nil
	})
	if errors.Is(err, jobs.ErrNotFound) {
		// Its record could not be created: it runs without one, as before,
		// and no pick-up can misread it.
		return nil
	}
	if err != nil {
		return fmt.Errorf("record checkpoint of job %s: %w", task.ID(), err)
	}
	return nil
}

// finish moves the record to the task's terminal state and alerts on a
// failure, which replaces the alert of an earlier interruption; a resumed job
// that completed clears that alert, and a long job that completed raises its
// notice when the settings ask. A resumed job its Resume hook left
// interrupted gets back the reason it was interrupted for (keepInterrupted).
// A canceled job reports nothing. A record already terminal, such as one
// interrupted at shutdown, is left as it is.
func (r *recorder) finish(ctx context.Context, task *bridgeipc.Task, resumedFrom jobs.Reason) {
	snapshot := task.Snapshot()
	var state jobs.State
	var result jsontext.Value
	switch snapshot.State {
	case bridgeipc.TaskStateCompleted:
		state = jobs.StateCompleted
		if snapshot.Result != nil {
			encoded, err := json.Marshal(snapshot.Result)
			if err != nil {
				slog.Warn("job result not encoded", "job_id", task.ID(), "error", err)
			}
			result = encoded
		}
	case bridgeipc.TaskStateFailed:
		state = jobs.StateFailed
	case bridgeipc.TaskStateInterrupted:
		state = jobs.StateInterrupted
	default:
		state = jobs.StateCanceled
	}
	var jobErr *jobs.Error
	if snapshot.Error != nil {
		jobErr = &jobs.Error{Code: snapshot.Error.Code, Message: snapshot.Error.Message}
	}
	record, err := r.h.cfg.Store.Finish(ctx, task.ID(), state, result, jobErr)
	if err != nil {
		slog.Error("job record not finished", "job_id", task.ID(), "route", task.Type(), "error", err)
		return
	}
	// Only this run's outcome alerts, never a record that was already terminal.
	resumed := resumedFrom != jobs.ReasonNone
	switch {
	case state == jobs.StateFailed && record.State == jobs.StateFailed && record.Error != nil:
		r.h.alert(ctx, record, alerts.SeverityError, "failed", record.Error.Message)
	case state == jobs.StateCompleted && record.State == jobs.StateCompleted:
		if resumed {
			r.h.resolveAlert(ctx, record.ID)
		}
		r.h.alertLongJob(ctx, record)
	case resumed && task.NotResumed() && record.State == jobs.StateInterrupted && record.Reason == jobs.ReasonNone:
		r.h.keepInterrupted(ctx, record.ID, resumedFrom)
	}
}
