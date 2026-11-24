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
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
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
	// pruneInterval is how often a running host prunes its store.
	pruneInterval = 24 * time.Hour
)

// resumes are the interrupted jobs a root host start resumes: the pick-ups
// of jobs whose host died or was stopped in this boot, which run before the
// host serves, and the resumes of jobs an earlier boot's shutdown or power
// loss interrupted, which run once it serves, as building their owner's
// handlers can hang (a home folder on a network mount at boot).
type resumes struct {
	inBoot, afterReboot []string
}

// reconcile marks the jobs this uid's last host left running as interrupted,
// by a reboot when that host ran in an earlier boot and by its crash
// otherwise, and prunes old records before the host takes new jobs. On the
// root host it returns the interrupted jobs to resume (toResume); any other
// interrupted job is never resumed. It also marks the scheduled runs that no
// longer run, which are never resumed.
func (h *Host) reconcile(ctx context.Context) (resumes, error) {
	// A record from before boot ids were kept counts as this boot's.
	thisBoot := func(e jobs.Executor) bool { return e.BootID == "" || e.BootID == h.executor.BootID }
	rebooted, err := h.cfg.Store.ReconcileCrashed(ctx, thisBoot, jobs.ReasonReboot)
	if err != nil {
		return resumes{}, fmt.Errorf("reconcile rebooted jobs: %w", err)
	}
	crashed, err := h.cfg.Store.ReconcileCrashed(ctx, jobs.ProcessAlive, jobs.ReasonCrash)
	if err != nil {
		return resumes{}, fmt.Errorf("reconcile crashed jobs: %w", err)
	}
	toResume, err := h.toResume(ctx, thisBoot)
	if err != nil {
		slog.Warn("interrupted jobs not listed for resume", "error", err)
	}
	for _, record := range slices.Concat(rebooted, crashed) {
		slog.Info("job marked interrupted", "job_id", record.ID, "route", record.Route, "reason", record.Reason)
		if !slices.Contains(toResume.inBoot, record.ID) && !slices.Contains(toResume.afterReboot, record.ID) {
			h.alertInterrupted(ctx, record, "")
		}
	}
	if h.cfg.Scheduled != nil {
		if _, err := h.scheduledRuns().Running(ctx); err != nil {
			slog.Warn("scheduled runs not checked at job host start", "error", err)
		}
	}
	h.prune(ctx)
	return toResume, nil
}

// prune applies the store's retention, at host start and then daily.
func (h *Host) prune(ctx context.Context) {
	h.pruned = h.cfg.Now()
	if err := h.cfg.Store.Prune(ctx, h.pruned); err != nil {
		slog.Warn("job record pruning failed", "error", err)
	}
}

// toResume lists, on the root host, the interrupted jobs their owner did not
// cancel that this start resumes (resumesNow). It clears Resumable on every
// other interrupted record, so a job is resumed at the one start after its
// interruption, and a cancel is never resumed.
func (h *Host) toResume(ctx context.Context, thisBoot func(jobs.Executor) bool) (resumes, error) {
	records, err := h.cfg.Store.List(ctx)
	if err != nil {
		return resumes{}, err
	}
	var ids resumes
	for _, record := range records {
		if record.State != jobs.StateInterrupted || !record.Resumable {
			continue
		}
		inBoot := thisBoot(record.Executor)
		if h.cfg.UID == 0 && !record.CancelRequested && h.resumesNow(record, inBoot) {
			if inBoot {
				ids.inBoot = append(ids.inBoot, record.ID)
			} else {
				ids.afterReboot = append(ids.afterReboot, record.ID)
			}
			continue
		}
		if _, err := h.cfg.Store.Update(ctx, record.ID, func(r *jobs.Record) error {
			r.Resumable = false
			return nil
		}); err != nil {
			slog.Warn("job resumable flag not cleared", "job_id", record.ID, "error", err)
			continue
		}
		slog.Debug("job not resumable after this start", "job_id", record.ID, "route", record.Route, "reason", record.Reason)
	}
	return ids, nil
}

// resumesNow reports whether this start resumes an interrupted record: one the
// shutdown or power loss of an earlier boot interrupted, of a route that
// resumes after a reboot, and, in this boot, one whose host crashed or was
// stopped by hand, of a route that resumes after that.
func (h *Host) resumesNow(record jobs.Record, thisBoot bool) bool {
	at := h.resumeAt(record.Route)
	if !thisBoot {
		return at&bridgeipc.ResumeAfterReboot != 0 && (record.Reason == jobs.ReasonShutdown || record.Reason == jobs.ReasonReboot)
	}
	switch record.Reason {
	case jobs.ReasonCrash:
		// A crash of a resumed run may be the resume's own doing; picking it
		// up again could crash every host start of the boot.
		return at&bridgeipc.ResumeAfterCrash != 0 && !record.Resumed
	case jobs.ReasonShutdown:
		return at&bridgeipc.ResumeAfterStop != 0
	default:
		return false
	}
}

// resumeAt is when route resumes. A route this build no longer resumes counts
// as one that resumes after a reboot, so that its failed resume is alerted.
func (h *Host) resumeAt(route string) bridgeipc.ResumeAt {
	if r, ok := h.cfg.Router.Route(route); ok && r.Resume != nil {
		return r.ResumesAt()
	}
	return bridgeipc.ResumeAfterReboot
}

// resumeInterrupted starts the Resume hook of each job reconcile listed. A
// host told to stop meanwhile leaves the rest as they are, for its next start
// to resume under the same rule.
func (h *Host) resumeInterrupted(ctx context.Context, ids []string) {
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if _, err := h.resume(ctx, id); err != nil {
			h.giveUpResume(ctx, id, err)
		}
	}
}

// giveUpResume alerts on a job this host could not resume, an error since it
// will never finish, and stamps it with this host's executor, so later host
// starts of this boot leave a job of a route that resumes only after a reboot
// alone. One of a route that resumes in the boot also stops being resumable,
// or each later start would try it again.
func (h *Host) giveUpResume(ctx context.Context, id string, cause error) {
	slog.Warn("interrupted job not resumed", "job_id", id, "error", cause)
	stamped := false
	record, err := h.cfg.Store.Update(ctx, id, func(r *jobs.Record) error {
		if r.State == jobs.StateInterrupted {
			r.Executor, stamped = h.executor, true
			if h.resumeAt(r.Route)&bridgeipc.ResumeInBoot != 0 {
				r.Resumable = false
			}
		}
		return nil
	})
	if err != nil {
		slog.Warn("job not stamped after a failed resume", "job_id", id, "error", err)
		return
	}
	if stamped {
		h.alert(ctx, record, alerts.SeverityError, "was interrupted", fmt.Sprintf("It could not be resumed: %v.", cause))
	}
}

// keepInterrupted gives a job whose Resume hook found nothing to follow back
// the reason it was interrupted for, stops it being resumable and raises the
// alert its interruption would have raised, with why it was not picked up.
// The task runner has logged why.
func (h *Host) keepInterrupted(ctx context.Context, id string, reason jobs.Reason) {
	record, err := h.cfg.Store.Update(ctx, id, func(r *jobs.Record) error {
		r.Reason, r.Resumable = reason, false
		return nil
	})
	if err != nil {
		slog.Warn("job interruption not recorded after a resume", "job_id", id, "error", err)
		return
	}
	why := ""
	if record.Error != nil {
		why = record.Error.Message
		if _, after, found := strings.Cut(why, bridgeipc.ErrNotResumed.Error()+": "); found {
			why = after
		}
	}
	h.alertInterrupted(ctx, record, why)
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
			if h.cfg.Now().Sub(h.pruned) >= pruneInterval {
				h.prune(ctx)
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
// IdleExit, or has neither once an update replaced its binary, sampled once a
// tick. The jobs are counted under the lock (the recorder's lock inside the
// host's, never the other way round): a bridge leaves only after its streams
// end, so every job it started counts by then. Deciding to exit
// closes the host to new bridges under the same lock, so none can join and
// start a job meanwhile.
func (h *Host) idle() bool {
	now := h.cfg.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.bridges > 0 || h.rec.running() > 0 {
		h.lastBusy = now
		return false
	}
	// A replaced host refuses every new bridge, so waiting serves no one.
	if now.Sub(h.lastBusy) < h.cfg.IdleExit && !h.cfg.Replaced() {
		return false
	}
	h.closing = true
	return true
}

// interruptRunning ends every running job at shutdown, suspending the
// resumable ones, and waits for their runners, and for the record of every job
// that ended just before, which Recorder.Finished writes before the task is
// Done. A job whose task ended before Started wrote its record, or before it
// had the recorder, is finished by Started instead, on the serve goroutine or
// a connection's, which the host waits for.
func (h *Host) interruptRunning() {
	ctx, cancel := jobs.DetachedContext(drainTimeout)
	defer cancel()
	running, ending := h.rec.interruptAll()
	var wg sync.WaitGroup
	for _, task := range running {
		wg.Go(func() { h.interrupt(ctx, task) })
	}
	wg.Wait()
	for _, task := range slices.Concat(running, ending) {
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
// terminal first, so the job's own Finish leaves it interrupted. A resumable
// task is marked interrupted before Suspend, so a handler the hook stops ends
// interrupted, and the record its Finish wrote gets the reason and checkpoint.
// A job its owner canceled is left to its handler, which ends it canceled, and
// a task that ended on its own, as a job completes during Suspend, to its own
// Finish, whether that reached the store first or not.
func (h *Host) interrupt(ctx context.Context, task *bridgeipc.Task) {
	if record, err := h.cfg.Store.Get(ctx, task.ID()); err == nil && record.CancelRequested {
		return
	}
	if route, ok := h.cfg.Router.Route(task.Type()); ok && route.Suspend != nil {
		task.BeginInterrupt()
	}
	checkpoint := h.suspend(ctx, task)
	marked := false
	record, err := h.cfg.Store.Update(ctx, task.ID(), func(r *jobs.Record) error {
		// Read under the store lock, which Finish takes after the task ended;
		// the task lock is never held while waiting for the store's.
		switch task.Snapshot().State {
		case bridgeipc.TaskStateCompleted, bridgeipc.TaskStateFailed, bridgeipc.TaskStateCanceled:
			return nil
		case bridgeipc.TaskStateQueued, bridgeipc.TaskStateRunning, bridgeipc.TaskStateInterrupted:
			// Still running, or ended by this shutdown's Suspend: marked below.
		}
		switch {
		case r.State == jobs.StateRunning:
			r.State, r.FinishedAt = jobs.StateInterrupted, h.cfg.Now().UTC()
		case r.State != jobs.StateInterrupted || r.Reason != jobs.ReasonNone:
			// It ended on its own during Suspend, such as completed.
			return nil
		}
		r.Reason = jobs.ReasonShutdown
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
		h.alertInterrupted(ctx, record, "")
	}
}

// suspend asks a resumable job for its checkpoint, unless its owner already
// asked to cancel it. It returns nil when the job is not suspended.
func (h *Host) suspend(ctx context.Context, task *bridgeipc.Task) jsontext.Value {
	if route, ok := h.cfg.Router.Route(task.Type()); !ok || route.Suspend == nil {
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
	router, err := h.ownerRouter(record.Owner)
	if err != nil {
		slog.Warn("job not suspended", "job_id", task.ID(), "error", err)
		return nil
	}
	route, ok := router.Route(task.Type())
	if !ok || route.Suspend == nil {
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

// resume starts a task that runs the Resume hook of
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
	router, err := h.ownerRouter(record.Owner)
	if err != nil {
		return nil, fmt.Errorf("handlers for job %s: %w", id, err)
	}
	route, ok := router.Route(record.Route)
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
	task, err := router.StartResume(route, raw, owner, id)
	if err != nil {
		return nil, err
	}
	slog.Info("job resumed", "job_id", id, "route", record.Route)
	return task.Snapshot(), nil
}

// ownerRouter returns the router whose handlers act for a job's owner: on the
// root host one built for that user, else the host's own.
func (h *Host) ownerRouter(owner jobs.Owner) (*bridgeipc.Router, error) {
	if h.cfg.Handlers == nil || h.cfg.UID != 0 {
		return h.cfg.Router, nil
	}
	return h.cfg.Handlers(&session.Session{
		User:       session.User{Username: owner.Username, UID: owner.UID, GID: owner.GID},
		Privileged: true,
		Timing:     session.Timing{CreatedAt: h.cfg.Now()},
	})
}

// rerun moves the interrupted record of a resumed job back to running in this
// host and returns the reason it was interrupted for. It keeps the start time,
// so the resumed run is never taken for a job under TransientMaxDuration, and
// the checkpoint.
func (h *Host) rerun(ctx context.Context, id string) (jobs.Reason, error) {
	var reason jobs.Reason
	_, err := h.cfg.Store.Update(ctx, id, func(r *jobs.Record) error {
		if r.State != jobs.StateInterrupted {
			return fmt.Errorf("job %s is %s, not interrupted", id, r.State)
		}
		reason = r.Reason
		r.State, r.Reason, r.FinishedAt, r.Executor = jobs.StateRunning, jobs.ReasonNone, time.Time{}, h.executor
		r.Error, r.Resumed = nil, true
		return nil
	})
	if err != nil {
		return jobs.ReasonNone, err
	}
	return reason, nil
}
