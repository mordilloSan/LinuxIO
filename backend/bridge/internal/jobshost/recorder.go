package jobshost

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
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
	// jobs holds the started jobs that have not finished; a job leaves it
	// when it finishes, after which its progress is ignored.
	jobs map[string]*job
}

type job struct {
	task *bridgeipc.Task
	// created: Started has written the record. finished: Finished came
	// first and left finishing the record to Started.
	created   bool
	finished  bool
	pending   jsontext.Value
	lastWrite time.Time
	timer     *time.Timer
}

func (r *recorder) running() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.jobs)
}

func (r *recorder) runningTasks() []*bridgeipc.Task {
	r.mu.Lock()
	defer r.mu.Unlock()
	tasks := make([]*bridgeipc.Task, 0, len(r.jobs))
	for _, j := range r.jobs {
		tasks = append(tasks, j.task)
	}
	return tasks
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
	r.mu.Unlock()

	ctx, cancel := jobs.DetachedContext(storeTimeout)
	defer cancel()
	err := r.h.cfg.Store.Create(ctx, r.h.newRecord(task, raw))
	if errors.Is(err, jobs.ErrExists) {
		err = r.h.rerun(ctx, id) // a resumed job keeps its record
	}
	if err != nil {
		slog.Error("job record not created", "job_id", id, "route", task.Type(), "error", err)
	}

	r.mu.Lock()
	j.created = true
	finish := j.finished || task.IsTerminal()
	if finish && r.jobs[id] == j {
		delete(r.jobs, id)
	}
	r.mu.Unlock()
	if finish {
		r.finish(ctx, task)
	}
}

func (h *Host) newRecord(task *bridgeipc.Task, raw jsontext.Value) jobs.Record {
	now := h.cfg.Now().UTC()
	owner := task.Owner()
	route, _ := h.cfg.Router.Route(task.Type())
	record := jobs.Record{
		ID:        task.ID(),
		Route:     task.Type(),
		Owner:     jobs.Owner{UID: h.ownerUID(owner.SessionID, owner.UID), Username: owner.Username},
		State:     jobs.StateRunning,
		CreatedAt: now,
		StartedAt: now,
		Request:   raw,
		Executor:  h.executor,
		Resumable: route.Resume != nil,
		Protocol:  jobs.Protocol,
	}
	if metadata := task.Snapshot().Metadata; metadata != nil {
		record.Target = metadata.Identity
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
	if !ok {
		return
	}
	j.pending = data
	if j.timer != nil {
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
	if err := r.h.cfg.Store.WriteProgress(id, j.pending); err != nil {
		slog.Debug("job progress not written", "job_id", id, "error", err)
	}
	j.pending, j.lastWrite = nil, time.Now()
}

// Finished flushes a job's last progress and finishes its record, unless
// Started has not written it yet.
func (r *recorder) Finished(task *bridgeipc.Task) {
	id := task.ID()
	r.mu.Lock()
	j, ok := r.jobs[id]
	if !ok {
		r.mu.Unlock()
		return
	}
	delete(r.jobs, id)
	if j.timer != nil {
		j.timer.Stop()
		j.timer = nil
	}
	r.writeProgressLocked(id, j)
	j.finished = true
	created := j.created
	r.mu.Unlock()
	if created {
		ctx, cancel := jobs.DetachedContext(storeTimeout)
		defer cancel()
		r.finish(ctx, task)
	}
}

// finish moves the record to the task's terminal state and alerts on a
// failure. A record already terminal, such as one interrupted at shutdown,
// is left as it is.
func (r *recorder) finish(ctx context.Context, task *bridgeipc.Task) {
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
	// Only this run's failure alerts, never a record that was already terminal.
	if state == jobs.StateFailed && record.State == jobs.StateFailed && record.Error != nil {
		r.h.alert(ctx, record.ID, record.Route+" failed", record.Error.Message)
	}
}
