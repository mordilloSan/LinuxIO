package jobshost

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
)

const (
	// suspendTimeout bounds one resumable job's Suspend at shutdown.
	suspendTimeout = 90 * time.Second
	// drainTimeout bounds the whole shutdown: suspends, record writes and
	// the wait for every runner to end.
	drainTimeout = 100 * time.Second
	// streamDrainTimeout bounds the wait for the Mutation streams to write
	// their terminal frame once the jobs ended.
	streamDrainTimeout = 5 * time.Second
)

// reconcile marks the jobs this uid's last host left running as interrupted,
// by a reboot when that host ran in an earlier boot and by its crash
// otherwise, and prunes old records before the host takes new jobs. On the
// root host it returns the resumable jobs an earlier boot interrupted, to
// resume once the host serves: once per boot, since a job interrupted in this
// boot was stopped on purpose or crashed the host.
func (h *Host) reconcile(ctx context.Context) ([]string, error) {
	// A record from before boot ids were kept counts as this boot's.
	thisBoot := func(e jobs.Executor) bool { return e.BootID == "" || e.BootID == h.executor.BootID }
	rebooted, err := h.cfg.Store.ReconcileCrashed(ctx, thisBoot, jobs.ReasonReboot)
	if err != nil {
		return nil, fmt.Errorf("reconcile rebooted jobs: %w", err)
	}
	crashed, err := h.cfg.Store.ReconcileCrashed(ctx, jobs.ProcessAlive, jobs.ReasonCrash)
	if err != nil {
		return nil, fmt.Errorf("reconcile crashed jobs: %w", err)
	}
	resumes, err := h.earlierBootResumes(ctx, thisBoot)
	if err != nil {
		slog.Warn("interrupted jobs not listed for resume", "error", err)
	}
	for _, record := range slices.Concat(rebooted, crashed) {
		slog.Info("job marked interrupted", "job_id", record.ID, "route", record.Route, "reason", record.Reason)
		if !slices.Contains(resumes, record.ID) {
			h.alertInterrupted(ctx, record)
		}
	}
	if err := h.cfg.Store.Prune(ctx, h.cfg.Now()); err != nil {
		slog.Warn("job record pruning failed", "error", err)
	}
	return resumes, nil
}

// earlierBootResumes lists, on the root host, the interrupted resumable jobs
// whose last executor ran in an earlier boot.
func (h *Host) earlierBootResumes(ctx context.Context, thisBoot func(jobs.Executor) bool) ([]string, error) {
	if h.cfg.UID != 0 {
		return nil, nil
	}
	records, err := h.cfg.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, record := range records {
		if record.State == jobs.StateInterrupted && record.Resumable && !thisBoot(record.Executor) {
			ids = append(ids, record.ID)
		}
	}
	return ids, nil
}

// resumeEarlierBoot starts the Resume hook of each job an earlier boot
// interrupted.
func (h *Host) resumeEarlierBoot(ctx context.Context, ids []string) {
	for _, id := range ids {
		if _, err := h.resume(ctx, id); err != nil {
			h.giveUpResume(ctx, id, err)
		}
	}
}

// giveUpResume alerts on a job this boot could not resume and stamps it with
// this host's executor, so later host starts in this boot leave it alone.
func (h *Host) giveUpResume(ctx context.Context, id string, cause error) {
	slog.Warn("interrupted job not resumed", "job_id", id, "error", cause)
	stamped := false
	record, err := h.cfg.Store.Update(ctx, id, func(r *jobs.Record) error {
		if r.State == jobs.StateInterrupted {
			r.Executor, stamped = h.executor, true
		}
		return nil
	})
	if err != nil {
		slog.Warn("job not stamped after a failed resume", "job_id", id, "error", err)
		return
	}
	if stamped {
		h.alert(ctx, id, record.Route+" was interrupted", fmt.Sprintf("It could not be resumed after the reboot: %v.", cause))
	}
}

// run waits for SIGTERM (ctx done), idle exit or a listener failure.
func (h *Host) run(ctx context.Context, ln *net.UnixListener, acceptErr <-chan error) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = ln.Close()
			h.interruptRunning()
			h.drainJobStreams()
			return nil
		case err := <-acceptErr:
			return fmt.Errorf("accept job host connection: %w", err)
		case <-ticker.C:
			if h.idle() {
				h.notify("STOPPING=1")
				return nil
			}
		}
	}
}

// counts returns the connected bridges and the running jobs.
func (h *Host) counts() (bridges, running int) {
	h.mu.Lock()
	bridges = h.bridges
	h.mu.Unlock()
	return bridges, h.rec.running()
}

// join counts a bridge in, or reports false once the host is closing.
func (h *Host) join() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return false
	}
	h.bridges++
	h.lastBusy = h.cfg.Now()
	return true
}

func (h *Host) leave() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.bridges--
	h.lastBusy = h.cfg.Now()
}

// idle reports whether the host has had no bridge and no running job for
// IdleExit, sampled once a tick. Deciding to exit closes the host to new
// bridges under the same lock, so none can join and start a job meanwhile.
func (h *Host) idle() bool {
	running := h.rec.running()
	now := h.cfg.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.bridges > 0 || running > 0 {
		h.lastBusy = now
		return false
	}
	if now.Sub(h.lastBusy) < h.cfg.IdleExit {
		return false
	}
	h.closing = true
	return true
}

// interruptRunning ends every running job at shutdown, suspending the
// resumable ones, and waits for their runners.
func (h *Host) interruptRunning() {
	ctx, cancel := jobs.DetachedContext(drainTimeout)
	defer cancel()
	tasks := h.rec.runningTasks()
	var wg sync.WaitGroup
	for _, task := range tasks {
		wg.Go(func() { h.interrupt(ctx, task) })
	}
	wg.Wait()
	for _, task := range tasks {
		select {
		case <-task.Done():
		case <-ctx.Done():
			slog.Warn("job host stopped before a job ended", "job_id", task.ID(), "route", task.Type())
		}
	}
}

// drainJobStreams closes the host to new connections and waits, at most
// streamDrainTimeout, for the open Mutation streams to end, so a client
// attached to an interrupted job reads its terminal frame before the
// sessions close.
func (h *Host) drainJobStreams() {
	h.mu.Lock()
	h.closing = true
	h.mu.Unlock()
	drained := make(chan struct{})
	go func() {
		h.jobStreams.Wait()
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(streamDrainTimeout):
		slog.Warn("job host stopped before every job stream ended")
	}
}

// interrupt marks a running job interrupted by the shutdown, with the
// checkpoint of a resumable job, then interrupts its task. The record turns
// terminal first, so the job's own Finish leaves it interrupted.
func (h *Host) interrupt(ctx context.Context, task *bridgeipc.Task) {
	checkpoint := h.suspend(ctx, task)
	marked := false
	record, err := h.cfg.Store.Update(ctx, task.ID(), func(r *jobs.Record) error {
		if r.State != jobs.StateRunning {
			return nil
		}
		r.State, r.Reason, r.FinishedAt = jobs.StateInterrupted, jobs.ReasonShutdown, h.cfg.Now().UTC()
		if checkpoint != nil {
			r.Checkpoint = checkpoint
		}
		marked = true
		return nil
	})
	task.Interrupt()
	if err != nil && !errors.Is(err, jobs.ErrNotFound) {
		slog.Warn("job not marked interrupted", "job_id", task.ID(), "error", err)
	}
	if marked {
		h.alertInterrupted(ctx, record)
	}
}

// suspend asks a resumable job for its checkpoint, unless its owner already
// asked to cancel it. It returns nil when the job is not suspended.
func (h *Host) suspend(ctx context.Context, task *bridgeipc.Task) jsontext.Value {
	route, ok := h.cfg.Router.Route(task.Type())
	if !ok || route.Suspend == nil {
		return nil
	}
	record, err := h.cfg.Store.Get(ctx, task.ID())
	if err != nil {
		slog.Warn("job record unreadable for suspend", "job_id", task.ID(), "error", err)
		return nil
	}
	if record.State != jobs.StateRunning || record.CancelRequested {
		return nil
	}
	raw, err := json.Marshal(record)
	if err != nil {
		slog.Warn("job record not encoded for suspend", "job_id", task.ID(), "error", err)
		return nil
	}
	suspendCtx, cancel := context.WithTimeout(ctx, suspendTimeout)
	defer cancel()
	checkpoint, err := route.Suspend(suspendCtx, raw)
	if err != nil {
		slog.Warn("job suspend failed", "job_id", task.ID(), "route", task.Type(), "error", err)
		return nil
	}
	return checkpoint
}

// resume serves jobs.resume: it starts a task that runs the Resume hook of
// the interrupted resumable job id on its record, under the job's own ID.
func (h *Host) resume(ctx context.Context, id string) (any, error) {
	if msg, code := h.refusal(ctx); code != 0 {
		return nil, bridgeipc.NewError(msg, code)
	}
	record, err := h.cfg.Store.Get(ctx, id)
	if errors.Is(err, jobs.ErrNotFound) {
		return nil, fmt.Errorf("%w: job %s", bridgeipc.ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("read job %s: %w", id, err)
	}
	if record.State != jobs.StateInterrupted || !record.Resumable {
		return nil, fmt.Errorf("%w: job %s is %s, not an interrupted resumable job", bridgeipc.ErrConflict, id, record.State)
	}
	route, ok := h.cfg.Router.Route(record.Route)
	if !ok {
		return nil, fmt.Errorf("%w: route %s", bridgeipc.ErrNotFound, record.Route)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode job %s: %w", id, err)
	}
	// The task's uid is the permission key, the host's; the record keeps
	// naming the user who started the job.
	owner := bridgeipc.TaskOwner{Username: record.Owner.Username, UID: h.cfg.UID}
	task, err := h.cfg.Router.StartResume(route, raw, owner, id)
	if err != nil {
		return nil, err
	}
	slog.Info("job resumed", "job_id", id, "route", record.Route)
	return task.Snapshot(), nil
}

// rerun moves the interrupted record of a resumed job back to running in this
// host. It keeps the start time, so the resumed run is never taken for a job
// under TransientMaxDuration, and the checkpoint.
func (h *Host) rerun(ctx context.Context, id string) error {
	_, err := h.cfg.Store.Update(ctx, id, func(r *jobs.Record) error {
		if r.State != jobs.StateInterrupted {
			return fmt.Errorf("job %s is %s, not interrupted", id, r.State)
		}
		r.State, r.Reason, r.FinishedAt, r.Executor = jobs.StateRunning, jobs.ReasonNone, time.Time{}, h.executor
		return nil
	})
	return err
}
