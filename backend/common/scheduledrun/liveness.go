package scheduledrun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
)

// activeTimeout bounds each question to systemd, so a wedged manager cannot
// stall a listing; a variable so tests can shorten it.
var activeTimeout = 5 * time.Second

// Liveness tells the scheduled runs that still run from the records a run
// left running: one of an earlier boot, which the reboot interrupted, and
// one systemd no longer runs, whose end hook never ran (a crash).
type Liveness struct {
	Store  *jobs.Store
	BootID string
	// Active reports whether unit still runs invocationID.
	Active func(ctx context.Context, unit, invocationID string) (bool, error)
	Alert  func(ctx context.Context, obs alerts.Observation) error // nil raises none
	Now    func() time.Time
}

// Running returns the scheduled runs that still run, after marking the
// others interrupted.
func (l Liveness) Running(ctx context.Context) ([]jobs.Record, error) {
	records, err := l.Store.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list scheduled runs: %w", err)
	}
	var running []jobs.Record
	for _, record := range records {
		reason, err := l.ended(ctx, record)
		if err != nil {
			return nil, err
		}
		if reason == jobs.ReasonNone {
			running = append(running, record)
			continue
		}
		l.markInterrupted(ctx, record, reason)
	}
	return running, nil
}

// ended says why the run of a running record has ended: the reboot for a run
// of an earlier boot, which systemd is not asked about, a crash for one
// systemd no longer runs. It is ReasonNone while the run runs and when
// systemd cannot be asked, or does not answer within activeTimeout. Its error
// is only ctx's.
func (l Liveness) ended(ctx context.Context, record jobs.Record) (jobs.Reason, error) {
	if record.Executor.BootID != l.BootID {
		return jobs.ReasonReboot, nil
	}
	askCtx, cancel := context.WithTimeout(ctx, activeTimeout)
	active, err := l.Active(askCtx, record.Executor.Unit, record.Executor.InvocationID)
	cancel()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return jobs.ReasonNone, ctxErr
	}
	switch {
	case err != nil:
		slog.Debug("scheduled run not checked with systemd", "job_id", record.ID, "unit", record.Executor.Unit, "error", err)
		return jobs.ReasonNone, nil
	case active:
		return jobs.ReasonNone, nil
	default:
		return jobs.ReasonCrash, nil
	}
}

// markInterrupted marks a run interrupted for reason, unless its end hook
// finished the record since it was listed, and raises the alert its kind
// raises. A caller that goes away does not leave a mark without its alert.
func (l Liveness) markInterrupted(ctx context.Context, record jobs.Record, reason jobs.Reason) {
	ctx = context.WithoutCancel(ctx)
	now := l.Now().UTC()
	marked := false
	updated, err := l.Store.Update(ctx, record.ID, func(r *jobs.Record) error {
		if r.State == jobs.StateRunning {
			r.State, r.Reason, r.FinishedAt = jobs.StateInterrupted, reason, now
			marked = true
		}
		return nil
	})
	if errors.Is(err, jobs.ErrNotFound) {
		return // ended and pruned since it was listed
	}
	if err != nil {
		slog.Warn("scheduled run not marked interrupted", "job_id", record.ID, "error", err)
		return
	}
	if !marked {
		return
	}
	slog.Info("scheduled run marked interrupted", "job_id", record.ID, "unit", record.Executor.Unit, "reason", reason)
	if l.Alert == nil {
		return
	}
	// The label comes from the record; KindOf only decides the alert and its link.
	kind, ok := KindOf(updated.Executor.Unit, "")
	if !ok {
		return
	}
	// No report setting decides whether an interruption is raised, so none
	// are read; its level then follows the levels grid like any other.
	if obs, ok := alertFor(kind, updated, nil); ok {
		if err := l.Alert(ctx, obs); err != nil {
			slog.Warn("scheduled run alert failed", "job_id", record.ID, "error", err)
		}
	}
}
