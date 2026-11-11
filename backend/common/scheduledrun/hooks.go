package scheduledrun

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// outputTimeout bounds the copy of a run's output, so a slow journal leaves
// time for the alert. Tests shorten it.
var outputTimeout = 5 * time.Second

// Result is systemd's $SERVICE_RESULT, $EXIT_CODE and $EXIT_STATUS.
type Result struct{ Service, Code, Status string }

// Hooks records the runs of scheduled units. Its fields are what the real
// hooks use; tests replace them.
type Hooks struct {
	Store       *jobs.Store
	ScheduleDir string
	Now         func() time.Time
	BootID      func() (string, error)
	Output      func(ctx context.Context, invocationID string, w io.Writer) error
	Stopping    func(ctx context.Context) bool
	Alert       func(ctx context.Context, obs alerts.Observation) error
	// Settings returns the notification settings; nil means the defaults.
	Settings func() alerts.NotificationSettings
}

// DefaultHooks is the production wiring.
func DefaultHooks() Hooks {
	return Hooks{
		Store:       jobs.NewScheduledStore(),
		ScheduleDir: filepath.Join(version.ConfigDir, "schedules"),
		Now:         time.Now,
		BootID:      jobs.BootID,
		Output:      journalOutput,
		Stopping:    systemd.SystemStopping,
		Alert:       alerts.Raise,
		Settings:    alerts.NotificationsOrDefaults,
	}
}

// Begin writes the running record of the run of unit that has invocationID.
// The record is fsynced: a run whose record a power loss took would get no
// "interrupted" record and no alert after the boot. A second Begin for the
// same run keeps the first record.
func (h Hooks) Begin(ctx context.Context, unit, invocationID string) error {
	record, err := h.newRecord(unit, invocationID)
	if err != nil {
		return err
	}
	record.State = jobs.StateRunning
	if err := h.Store.CreateSynced(ctx, record); err != nil && !errors.Is(err, jobs.ErrExists) {
		return fmt.Errorf("record scheduled run: %w", err)
	}
	return nil
}

// End finishes the record of the run, prunes the unit's older runs, saves a
// copy of the run's output and raises the alert or notice the script's
// outcome deserves, in that order, so a slow step cannot keep a quicker one
// from running: the output copy has its own limit (outputTimeout), and the
// alert, whose delivery may outlast the caller's deadline, comes last and is
// raised even when the caller's context has ended by then. It returns the
// errors of the steps after the record. Ending a run that has ended changes
// nothing.
func (h Hooks) End(ctx context.Context, unit, invocationID string, result Result) error {
	kind, ok := KindOf(unit, h.ScheduleDir)
	if !ok {
		return fmt.Errorf("unit %q is not scheduled work", unit)
	}
	record, changed, err := h.finish(ctx, unit, invocationID, result)
	if err != nil || !changed {
		return err
	}

	var errs []error
	if pruneErr := h.Store.PrunePerTarget(ctx, h.Now(), jobs.MaxScheduledRunsPerUnit); pruneErr != nil {
		errs = append(errs, fmt.Errorf("prune scheduled runs: %w", pruneErr))
	}
	outputCtx, cancel := context.WithTimeout(ctx, outputTimeout)
	outputErr := h.saveOutput(outputCtx, record.ID, invocationID)
	cancel()
	if outputErr != nil {
		errs = append(errs, outputErr)
	}
	if obs, ok := alertFor(kind, record, h.Settings); ok {
		// alerts.Raise bounds its own lock wait and delivery.
		if alertErr := h.Alert(context.WithoutCancel(ctx), obs); alertErr != nil {
			errs = append(errs, fmt.Errorf("raise alert: %w", alertErr))
		}
	}
	return errors.Join(errs...)
}

// RequestCancel marks the running record of unit as canceled by request, so
// End records the run as canceled. With an invocationID only the record of
// that run is marked, so a stop that arrives after its run ended cannot mark
// the next run; without one (a scrub cancel knows none) every running record
// of unit is. No running record is not an error.
func RequestCancel(ctx context.Context, store *jobs.Store, unit, invocationID string) error {
	return setCancelRequested(ctx, store, unit, invocationID, true)
}

// WithdrawCancel takes back RequestCancel after the stop it preceded failed.
// It matches records as RequestCancel does and leaves a run that has ended
// as it ended.
func WithdrawCancel(ctx context.Context, store *jobs.Store, unit, invocationID string) error {
	return setCancelRequested(ctx, store, unit, invocationID, false)
}

func setCancelRequested(ctx context.Context, store *jobs.Store, unit, invocationID string, requested bool) error {
	active, err := store.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("list running scheduled runs: %w", err)
	}
	var errs []error
	for _, record := range active {
		if record.Executor.Unit != unit || invocationID != "" && record.Executor.InvocationID != invocationID {
			continue
		}
		_, err := store.Update(ctx, record.ID, func(r *jobs.Record) error {
			if r.State == jobs.StateRunning {
				r.CancelRequested = requested
			}
			return nil
		})
		if err != nil && !errors.Is(err, jobs.ErrNotFound) {
			errs = append(errs, fmt.Errorf("update cancel request of scheduled run %s: %w", record.ID, err))
		}
	}
	return errors.Join(errs...)
}

// newRecord is the record of a run that has just started.
func (h Hooks) newRecord(unit, invocationID string) (jobs.Record, error) {
	kind, ok := KindOf(unit, h.ScheduleDir)
	if !ok {
		return jobs.Record{}, fmt.Errorf("unit %q is not scheduled work", unit)
	}
	id, err := jobs.UnitRunID(invocationID)
	if err != nil {
		return jobs.Record{}, err
	}
	boot, err := h.BootID()
	if err != nil {
		return jobs.Record{}, fmt.Errorf("read boot ID: %w", err)
	}
	metadata := struct {
		Label    string   `json:"label"`
		Identity []string `json:"identity,omitzero"`
	}{Label: kind.Label}
	if kind.Instance != "" {
		metadata.Identity = []string{kind.Instance}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return jobs.Record{}, fmt.Errorf("encode metadata: %w", err)
	}
	now := h.Now()
	return jobs.Record{
		ID:        id,
		Route:     kind.Route,
		Owner:     jobs.Owner{UID: 0, Username: "root"},
		Target:    []string{unit},
		CreatedAt: now,
		StartedAt: now,
		Metadata:  jsontext.Value(encoded),
		Executor:  jobs.Executor{Kind: jobs.ExecutorUnit, Unit: unit, InvocationID: invocationID, BootID: boot},
		Protocol:  jobs.Protocol,
	}, nil
}

// finish writes the terminal state of the run. changed is false when the
// record was terminal already. A run whose Begin never ran gets its record
// here.
func (h Hooks) finish(ctx context.Context, unit, invocationID string, result Result) (record jobs.Record, changed bool, err error) {
	id, err := jobs.UnitRunID(invocationID)
	if err != nil {
		return jobs.Record{}, false, err
	}
	// The manager is asked before the store is locked.
	stopping := result.Service != "success" && h.Stopping(ctx)
	now := h.Now()
	apply := func(r *jobs.Record) error {
		changed = r.State == jobs.StateRunning
		if !changed {
			return nil
		}
		r.State, r.Reason, r.Error = outcome(result, r.CancelRequested, stopping)
		r.FinishedAt = now
		return nil
	}

	record, err = h.Store.Update(ctx, id, apply)
	if errors.Is(err, jobs.ErrNotFound) {
		fresh, newErr := h.newRecord(unit, invocationID)
		if newErr != nil {
			return jobs.Record{}, false, newErr
		}
		fresh.State = jobs.StateRunning
		_ = apply(&fresh)
		if err = h.Store.Create(ctx, fresh); err == nil {
			return fresh, true, nil
		}
		if errors.Is(err, jobs.ErrExists) { // Begin ran meanwhile.
			record, err = h.Store.Update(ctx, id, apply)
		}
	}
	if err != nil {
		return jobs.Record{}, false, fmt.Errorf("finish scheduled run: %w", err)
	}
	return record, changed, nil
}

// outcome decides how a run ended. A run that succeeded did, whatever else
// happened; otherwise a cancel request wins over a host that is stopping.
func outcome(result Result, cancelRequested, stopping bool) (jobs.State, jobs.Reason, *jobs.Error) {
	switch {
	case result.Service == "success":
		return jobs.StateCompleted, jobs.ReasonNone, nil
	case cancelRequested:
		return jobs.StateCanceled, jobs.ReasonNone, &jobs.Error{Code: 499, Message: "operation aborted"}
	case stopping:
		return jobs.StateInterrupted, jobs.ReasonShutdown, nil
	}
	var message string
	switch result.Service {
	case "exit-code":
		message = "exited with status " + result.Status
	case "signal", "core-dump":
		message = "killed by signal " + result.Status
	case "timeout":
		message = "timed out"
	case "oom-kill":
		message = "killed for using too much memory"
	default:
		message = "failed: " + result.Service
	}
	return jobs.StateFailed, jobs.ReasonNone, &jobs.Error{Code: 500, Message: message}
}

// alertFor is the alert a run that ended raises, if any: one that failed, one
// the system stopped or that ended without its end hook (Liveness), whatever
// the report settings say, and one that completed when they ask for every run
// of its script. An interruption by a shutdown or a reboot is a warning, which
// follows the levels grid like any other; one whose end hook never reported
// (a crash) is an error, since its outcome is unknown. A script's completed
// runs are occurrences of one notice per schedule; a nil settings means the
// defaults.
func alertFor(kind Kind, record jobs.Record, settings func() alerts.NotificationSettings) (alerts.Observation, bool) {
	if !kind.Alerts {
		return alerts.Observation{}, false
	}
	// The name the run began with, in case the schedule was renamed since.
	label := kind.Label
	var metadata struct {
		Label string `json:"label"`
	}
	if json.Unmarshal(record.Metadata, &metadata) == nil && metadata.Label != "" {
		label = metadata.Label
	}
	obs := alerts.Observation{
		Source: AlertSource,
		Key:    record.ID,
		Link:   kind.Link,
	}
	switch record.State {
	case jobs.StateFailed:
		obs.Severity = alerts.SeverityError
		obs.Title = label + " failed"
		obs.Message = record.Error.Message
	case jobs.StateInterrupted:
		obs.Severity = alerts.SeverityWarning
		obs.Title = label + " was interrupted"
		obs.Message = "The system stopped before the run finished."
		if record.Reason == jobs.ReasonCrash {
			obs.Severity = alerts.SeverityError
			obs.Message = "The run ended without reporting its result."
		}
	case jobs.StateCompleted:
		n := alerts.DefaultNotifications()
		if settings != nil {
			n = settings()
		}
		if n.ReportForScript(kind.Instance) != alerts.ReportEveryRun {
			return alerts.Observation{}, false
		}
		obs.Key = FinishedAlertKey(kind.Instance)
		obs.Severity = alerts.SeverityInfo
		obs.Title = label + " finished"
		obs.Message = alerts.RanFor(record.FinishedAt.Sub(record.StartedAt))
		obs.OccurrenceID = record.ID
	default:
		return alerts.Observation{}, false
	}
	return obs, true
}

// FinishedAlertKey is the key, under AlertSource, of the one notice the
// completed runs of schedule scheduleID raise.
func FinishedAlertKey(scheduleID string) string { return "finished-" + scheduleID }

// saveOutput keeps a copy of the run's output next to its record. A run with
// no output leaves no file. When reading the output fails part-way, what was
// read is kept and the error is returned.
func (h Hooks) saveOutput(ctx context.Context, id, invocationID string) error {
	w := newHeadTailWriter()
	var readErr error
	if err := h.Output(ctx, invocationID, w); err != nil {
		readErr = fmt.Errorf("read run output: %w", err)
	}
	data := w.Bytes()
	if len(data) == 0 {
		return readErr
	}
	if err := atomicfile.Write(h.Store.OutputPath(id), data, 0o600); err != nil {
		return errors.Join(readErr, fmt.Errorf("save run output: %w", err))
	}
	return readErr
}
