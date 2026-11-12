package scheduledrun

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	// Resolve clears an alert; nil clears nothing.
	Resolve func(ctx context.Context, source, key string) error
	// UpdateNotifyHook reports whether the provider's own notify hook is
	// installed on the system update unit; nil means it is not.
	UpdateNotifyHook func(unit string) bool
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
		Resolve:     alerts.Resolve,
		UpdateNotifyHook: func(unit string) bool {
			_, err := os.Stat(UpdateNotifyDropIn(unit))
			return err == nil
		},
	}
}

// UpdateNotifyDropIn is the drop-in the auto-update settings write on the
// provider's update service unit to report its runs (`linuxio alert
// auto-update`). That package owns the path and tests it against this one.
func UpdateNotifyDropIn(unit string) string {
	return "/etc/systemd/system/" + unit + ".d/linuxio-alert.conf"
}

// The alert a failed automatic update raises, the same record whichever
// hook reports it: the provider's notify hook, which owns these names in
// the linuxio command and tests them against these, or End below.
const (
	UpdateAlertSource = "auto-update"
	UpdateAlertKey    = "run"
)

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
	if kind.worker() {
		if alertErr := h.reportWorkerRun(context.WithoutCancel(ctx), kind, record, result); alertErr != nil {
			errs = append(errs, alertErr)
		}
	}
	if obs, ok := h.failedUpdateAlert(kind, unit, invocationID, record, result); ok {
		if alertErr := h.Alert(context.WithoutCancel(ctx), obs); alertErr != nil {
			errs = append(errs, fmt.Errorf("raise alert: %w", alertErr))
		}
	}
	return errors.Join(errs...)
}

// failedUpdateAlert is the alert of a system update that failed on a host
// where the provider's notify hook is not installed (no notify policy was
// ever saved, or a "never" from an older build is stored): that hook reports
// a failed run under every policy it is installed with, so without it the
// failure is reported here, as the same record, and a failed automatic update
// is reported on every host. Each failed run is a new occurrence, named by
// its invocation ID as the provider's hook names it, so a run both hooks
// report (the drop-in removed, systemd not yet reloaded) is one occurrence.
func (h Hooks) failedUpdateAlert(kind Kind, unit, invocationID string, record jobs.Record, result Result) (alerts.Observation, bool) {
	if kind.Route != RouteSystemUpdate || record.State != jobs.StateFailed {
		return alerts.Observation{}, false
	}
	if h.UpdateNotifyHook != nil && h.UpdateNotifyHook(unit) {
		return alerts.Observation{}, false
	}
	return alerts.Observation{
		Source:       UpdateAlertSource,
		Key:          UpdateAlertKey,
		Severity:     alerts.SeverityError,
		Title:        "Automatic updates failed",
		Message:      "Service result: " + result.Service + ". The run's output is in the history.",
		Link:         "/history?job=" + record.ID,
		OccurrenceID: invocationID,
	}, true
}

// reportWorkerRun raises or clears the run alert of a worker unit (snapshots,
// resync, Docker updates) whose run ended as record says. The worker raises
// an alert for each rule, job, pool or container that fails and then exits
// with ExitReported; a run that failed in any other way (configuration that
// cannot be read, a Docker daemon that does not answer, a killed worker) has
// nothing of its own to report it under, so it is reported here, whatever the
// report settings say. The alert is one per unit and links to the latest such
// run in the history, where its output is. A run
// that completes, or that failed with everything reported, clears it; one the
// system stopped, that was canceled or that did no work (ExitSkipped) changes
// nothing.
func (h Hooks) reportWorkerRun(ctx context.Context, kind Kind, record jobs.Record, result Result) error {
	switch {
	case record.State == jobs.StateFailed && result.exited(ExitSkipped):
		// The run did no work: it neither failed nor cleared anything.
	case record.State == jobs.StateCompleted, record.State == jobs.StateFailed && result.exited(ExitReported):
		if h.Resolve == nil {
			return nil
		}
		if err := h.Resolve(ctx, AlertSource, runAlertKey(kind)); err != nil {
			return fmt.Errorf("resolve run alert: %w", err)
		}
	case record.State == jobs.StateFailed:
		reason := record.Error.Message
		message := strings.ToUpper(reason[:1]) + reason[1:] + ". The run's output is in the history."
		if err := h.Alert(ctx, runAlert(kind, record, reason, message)); err != nil {
			return fmt.Errorf("raise run alert: %w", err)
		}
	}
	return nil
}

// exited reports whether the worker exited with status.
func (r Result) exited(status int) bool {
	return r.Service == "exit-code" && r.Status == strconv.Itoa(status)
}

// runAlert is the run alert of the worker unit of kind for the run of record.
// reason is how the run failed, and is the alert's occurrence: a unit that
// keeps failing the same way is one occurrence, delivered once, whose link
// follows the latest run; one that fails in another way, or again after a run
// cleared the alert, is delivered again.
func runAlert(kind Kind, record jobs.Record, reason, message string) alerts.Observation {
	return alerts.Observation{
		Source:       AlertSource,
		Key:          runAlertKey(kind),
		Severity:     alerts.SeverityError,
		Title:        kind.Label + " failed",
		Message:      message,
		Link:         "/history?job=" + record.ID,
		OccurrenceID: reason,
	}
}

// runAlertKey is the key, under AlertSource, of the run alert of a worker
// unit: its route's name, and its instance when it has one.
func runAlertKey(kind Kind) string {
	key := "run-" + strings.ReplaceAll(strings.TrimPrefix(kind.Route, "scheduled."), "_", "-")
	if kind.Instance != "" {
		key += "-" + kind.Instance
	}
	return key
}

// ResyncRunAlertPool returns the pool whose resync unit raised the run alert
// key under AlertSource. The btrfs health check uses it to clear the run
// alert of a pool that is gone, since only the unit's next run clears it.
func ResyncRunAlertPool(key string) (string, bool) {
	uuid, ok := strings.CutPrefix(key, runAlertKey(Kind{Route: RouteResync})+"-")
	if !ok || jobs.ValidateID(uuid) != nil {
		return "", false
	}
	return uuid, true
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
