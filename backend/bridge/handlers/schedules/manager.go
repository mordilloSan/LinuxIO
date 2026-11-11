package schedules

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"
	"uuid"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

type manager struct {
	store store
	host  hostOps
}

func newManager() manager { return manager{defaultStore, nativeHost()} }

func (m manager) status(ctx context.Context, def apischema.ScheduleDefinition) apischema.ScheduleStatus {
	result := apischema.ScheduleStatus{Definition: def, UnitName: serviceName(def.ID), ActiveState: "unknown"}
	service, err := m.host.inspect(ctx, serviceName(def.ID))
	if err == nil {
		result.ActiveState = service.ActiveState
		if validInvocationID(service.InvocationID) {
			result.InvocationID = service.InvocationID
		}
		result.CanStop = active(service) && result.InvocationID != ""
		if service.ExitedAt != 0 {
			result.Result = service.Result
			result.ExitCode = &service.ExitCode
			result.ExitStatus = &service.ExitStatus
		}
		if service.ActiveState == "failed" {
			result.Result = service.Result
		}
	}
	timer, timerErr := m.host.inspect(ctx, timerName(def.ID))
	result.NextRunAt, result.LastRunAt = timer.NextRunAt, timer.LastRunAt
	if err == nil {
		err = timerErr
	}
	if err == nil && def.Enabled && !active(timer) {
		err = errors.New("timer is inactive; enable again to reapply configuration")
	}
	if err != nil {
		message := err.Error()
		result.Error = &message
	}
	return result
}

func (m manager) list(ctx context.Context) (apischema.SchedulesListResult, error) {
	result := apischema.SchedulesListResult{Schedules: []apischema.ScheduleStatus{}}
	if err := m.host.available(); err != nil {
		message := err.Error()
		result.Error = &message
		return result, nil
	}
	defs, err := m.store.definitions(ctx)
	if err != nil {
		return result, err
	}
	result.Available = true
	choices := m.shownNotifyChoices()
	for _, def := range defs {
		status := m.status(ctx, def)
		status.Notify = choices[def.ID]
		result.Schedules = append(result.Schedules, status)
	}
	return result, nil
}

func (m manager) get(ctx context.Context, id string) (apischema.ScheduleStatus, error) {
	def, err := m.store.definition(id)
	if err != nil {
		return apischema.ScheduleStatus{}, err
	}
	status := m.status(ctx, def)
	status.Notify = m.shownNotifyChoices()[id]
	return status, nil
}

// shownNotifyChoices are the choices list and get show. Settings it cannot
// read show none, so the schedules stay listed; that is logged at Debug, as
// the Schedules tab polls both and alerts.get_notifications warns.
func (m manager) shownNotifyChoices() map[string]string {
	choices, err := m.host.notifyChoices()
	if err != nil {
		slog.Debug("notification settings unreadable, showing no notification choices", "error", err)
	}
	return choices
}

func (m manager) create(ctx context.Context, req apischema.ScheduleCreateRequest) (apischema.ScheduleStatus, error) {
	if err := m.host.available(); err != nil {
		return apischema.ScheduleStatus{}, err
	}
	if err := validateNotify(req.Notify); err != nil {
		return apischema.ScheduleStatus{}, err
	}
	opts, account, err := normalizeOptions(ctx, req.Options, m.host)
	if err != nil {
		return apischema.ScheduleStatus{}, err
	}
	def := apischema.ScheduleDefinition{ID: uuid.NewV4().String(), Options: opts}
	err = m.store.locked(ctx, func() error {
		defs, listErr := m.store.definitions(ctx)
		if listErr != nil {
			return listErr
		}
		if len(defs) >= maxSchedules {
			return errors.New("at most 100 scheduled scripts are supported")
		}
		// The choice is stored first: a create that cannot store it changes
		// nothing.
		if req.Notify != "" {
			if notifyErr := m.host.setNotifyChoice(ctx, def.ID, req.Notify); notifyErr != nil {
				return notifyErr
			}
		}
		applyErr := m.apply(ctx, def, account)
		if applyErr != nil && req.Notify != "" {
			// A create that saved no definition takes its choice back; one that
			// did leaves a disabled task, repaired by saving it again, which
			// keeps it.
			if _, defErr := m.store.definition(def.ID); errors.Is(defErr, os.ErrNotExist) {
				m.takeBackNotifyChoice(ctx, def.ID)
			}
		}
		return applyErr
	})
	if err != nil {
		return apischema.ScheduleStatus{}, err
	}
	status := m.status(ctx, def)
	status.Notify = req.Notify
	return status, nil
}

// update replaces the options of a schedule and, when req names one that
// differs from the stored one, its notification choice. A change of only the
// choice leaves the definition and units alone, so it also works while the
// script runs.
func (m manager) update(ctx context.Context, req apischema.ScheduleUpdateRequest) error {
	if req.Notify != nil {
		if err := validateNotify(*req.Notify); err != nil {
			return err
		}
	}
	return m.store.locked(ctx, func() error {
		def, err := m.store.definition(req.ID)
		if err != nil {
			return err
		}
		notify := req.Notify
		if notify != nil {
			// Settings that cannot be read refuse any choice, even a clear:
			// what they store is unknown.
			choices, readErr := m.host.notifyChoices()
			if readErr != nil {
				return readErr
			}
			if *notify == choices[req.ID] {
				notify = nil
			}
		}
		if notify != nil && sameOptions(req.Options, def.Options) {
			return m.host.setNotifyChoice(ctx, req.ID, *notify)
		}
		return m.changeLocked(ctx, req.ID, &req.Options, notify, nil, false)
	})
}

// A choice left behind names no schedule and is harmless, so the two
// clean-ups below log a failure instead of returning it.

// takeBackNotifyChoice clears the choice stored for a create that saved no
// definition.
func (m manager) takeBackNotifyChoice(ctx context.Context, id string) {
	if err := m.host.setNotifyChoice(ctx, id, ""); err != nil {
		slog.Warn("could not take back the notification choice of a scheduled task that was not created", "schedule", id, "error", err)
	}
}

// forgetNotifyChoice clears the choice of a deleted schedule, when it has one.
func (m manager) forgetNotifyChoice(ctx context.Context, id string) {
	choices, err := m.host.notifyChoices()
	if _, ok := choices[id]; ok && err == nil {
		err = m.host.setNotifyChoice(ctx, id, "")
	}
	if err != nil {
		slog.Warn("could not clear the notification choice of a deleted scheduled task", "schedule", id, "error", err)
	}
}

// A disabled desired definition survives partial writes/reload failures, so the
// administrator can retry the same update. Units reference immutable scripts;
// a partial update cannot combine a new script with an old execution account.
func (m manager) apply(ctx context.Context, def apischema.ScheduleDefinition, account *user.User) error {
	enabled := def.Enabled
	def.Enabled = false
	if err := m.store.save(def); err != nil {
		return err
	}
	if err := secureDirectory(filepath.Join(m.store.configDir, "scripts"), 0o755); err != nil {
		return err
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return err
	}
	if err := atomicfile.Write(m.store.scriptPath(def), []byte(def.Options.Script), 0o440, os.Geteuid(), gid); err != nil {
		return fmt.Errorf("save managed script: %w", err)
	}
	if err := atomicfile.Write(filepath.Join(m.store.unitDir, serviceName(def.ID)), m.store.renderService(def, account), 0o644); err != nil {
		return err
	}
	if err := atomicfile.Write(filepath.Join(m.store.unitDir, timerName(def.ID)), renderTimer(def), 0o644); err != nil {
		return err
	}
	if err := m.host.reload(ctx); err != nil {
		return fmt.Errorf("reload scheduled script units: %w", err)
	}
	if enabled {
		if err := m.host.enable(ctx, timerName(def.ID)); err != nil {
			return err
		}
		// Save desired enablement before starting: a lost reply is discoverable on
		// the read path, without automatically retrying a potentially accepted job.
		def.Enabled = true
		if err := m.store.save(def); err != nil {
			return err
		}
		if err := m.host.start(ctx, timerName(def.ID)); err != nil {
			return err
		}
	}
	return m.store.removeScripts(def.ID, m.store.scriptPath(def))
}

func (m manager) pauseTimer(ctx context.Context, id string) error {
	timer, err := m.host.inspect(ctx, timerName(id))
	if err != nil {
		return err
	}
	if timer.Exists {
		if err := m.host.stop(ctx, timerName(id)); err != nil {
			return err
		}
		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			state, err := m.host.inspect(waitCtx, timerName(id))
			if err != nil {
				return err
			}
			if !active(state) {
				break
			}
			select {
			case <-waitCtx.Done():
				return waitCtx.Err()
			case <-ticker.C:
			}
		}
	}
	return nil
}

func (m manager) change(ctx context.Context, id string, options *apischema.ScheduleOptions, enabled *bool, deleting bool) error {
	return m.store.locked(ctx, func() error {
		return m.changeLocked(ctx, id, options, nil, enabled, deleting)
	})
}

// changeLocked stores notify, when given, after the checks that can refuse the
// change and before the schedule changes, so a choice that cannot be stored
// leaves the schedule as it was.
func (m manager) changeLocked(ctx context.Context, id string, options *apischema.ScheduleOptions, notify *string, enabled *bool, deleting bool) error {
	def, err := m.store.definition(id)
	if err != nil {
		return err
	}
	candidate := def.Options
	if options != nil {
		candidate = *options
	}
	var account *user.User
	if !deleting && (enabled == nil || *enabled) {
		candidate, account, err = normalizeOptions(ctx, candidate, m.host)
		if err != nil {
			return err
		}
	}
	if err := m.pauseTimer(ctx, id); err != nil {
		return err
	}
	if options != nil || deleting || (enabled != nil && *enabled) {
		if err := m.requireIdle(ctx, def); err != nil {
			return err
		}
	}
	if err := m.storeNotifyChoice(ctx, def, notify); err != nil {
		return err
	}
	// Remove boot enablement before modifying files. Failure leaves a disabled,
	// recoverable definition rather than activating partially written units.
	if err := m.host.disable(ctx, timerName(id)); err != nil {
		return err
	}
	if deleting {
		return m.delete(ctx, def)
	}
	def.Options = candidate
	if enabled != nil {
		def.Enabled = *enabled
	}
	if enabled != nil && !*enabled {
		return m.store.save(def)
	}
	return m.apply(ctx, def, account)
}

// storeNotifyChoice stores notify, when given, while the timer of def is
// paused. One that cannot be stored resumes the timer, as a refused change
// does.
func (m manager) storeNotifyChoice(ctx context.Context, def apischema.ScheduleDefinition, notify *string) error {
	if notify == nil {
		return nil
	}
	err := m.host.setNotifyChoice(ctx, def.ID, *notify)
	if err == nil || !def.Enabled {
		return err
	}
	if startErr := m.host.start(ctx, timerName(def.ID)); startErr != nil {
		return fmt.Errorf("%w; restore timer: %w", err, startErr)
	}
	return err
}

func (m manager) requireIdle(ctx context.Context, def apischema.ScheduleDefinition) error {
	state, err := m.host.inspect(ctx, serviceName(def.ID))
	if err != nil {
		return err
	}
	if !active(state) {
		return nil
	}
	if def.Enabled {
		if err := m.host.start(ctx, timerName(def.ID)); err != nil {
			return fmt.Errorf("script is running; restore timer: %w", err)
		}
	}
	return errors.New("script is running or queued; stop it or wait before changing its configuration")
}

func (m manager) delete(ctx context.Context, def apischema.ScheduleDefinition) error {
	def.Enabled = false
	if err := m.store.save(def); err != nil {
		return err
	}
	for _, name := range []string{timerName(def.ID), serviceName(def.ID)} {
		if err := removeFile(filepath.Join(m.store.unitDir, name)); err != nil {
			return err
		}
	}
	if err := m.host.reload(ctx); err != nil {
		return err
	}
	if err := m.store.removeScripts(def.ID, ""); err != nil {
		return err
	}
	path, err := m.store.definitionPath(def.ID)
	if err != nil {
		return err
	}
	if err := removeFile(path); err != nil {
		return err
	}
	m.forgetNotifyChoice(ctx, def.ID)
	resolveFinishedNotice(ctx, def.ID)
	return nil
}

// resolveFinishedNotice clears the notice the completed runs of a deleted
// schedule raised, which links to a task that is gone. One that cannot be
// cleared is logged, as the choice is: the schedule is deleted either way.
func resolveFinishedNotice(ctx context.Context, id string) {
	if err := alerts.Resolve(ctx, scheduledrun.AlertSource, scheduledrun.FinishedAlertKey(id)); err != nil {
		slog.Warn("could not resolve the finished notice of a deleted scheduled task", "schedule", id, "error", err)
	}
}

func (m manager) runNow(ctx context.Context, id string) error {
	return m.store.locked(ctx, func() error {
		def, err := m.store.definition(id)
		if err != nil {
			return err
		}
		_, account, err := normalizeOptions(ctx, def.Options, m.host)
		if err != nil {
			return err
		}
		state, err := m.host.inspect(ctx, serviceName(id))
		if err != nil {
			return err
		}
		if active(state) {
			return errors.New("script already has an active or queued run")
		}
		if err := m.ensureInstalled(ctx, def, account); err != nil {
			return err
		}
		// Reload repairs a previously interrupted reload; StartUnit is deliberately
		// not retried because a lost reply could otherwise execute a short job twice.
		if err := m.host.reload(ctx); err != nil {
			return err
		}
		return m.host.start(ctx, serviceName(id))
	})
}

// ensureInstalled checks that the unit and script on disk match def. Schedules
// saved by an older build hash to a different script name, so a missing script
// is re-applied first; the caller already holds the store lock.
func (m manager) ensureInstalled(ctx context.Context, def apischema.ScheduleDefinition, account *user.User) error {
	if _, statErr := os.Stat(m.store.scriptPath(def)); errors.Is(statErr, os.ErrNotExist) {
		if err := m.apply(ctx, def, account); err != nil {
			return err
		}
	}
	unit, err := os.ReadFile(filepath.Join(m.store.unitDir, serviceName(def.ID)))
	if err != nil {
		return err
	}
	script, err := os.ReadFile(m.store.scriptPath(def))
	if err != nil {
		return err
	}
	if !bytes.Equal(unit, m.store.renderService(def, account)) || !bytes.Equal(script, []byte(def.Options.Script)) {
		return errors.New("scheduled script configuration is incomplete; save the task again before running")
	}
	return nil
}

func (m manager) stop(ctx context.Context, req apischema.ScheduleStopRequest) error {
	if !validInvocationID(req.InvocationID) {
		return errors.New("invalid systemd invocation ID")
	}
	return m.store.locked(ctx, func() error {
		if _, err := m.store.definition(req.ID); err != nil {
			return err
		}
		// The mark names the run, so a stop that comes after its run ended
		// cannot mark the next one. A stop that fails takes it back.
		unit := serviceName(req.ID)
		if err := m.host.markCanceled(ctx, unit, req.InvocationID); err != nil {
			slog.Warn("could not mark the scheduled run canceled", "unit", unit, "invocation_id", req.InvocationID, "error", err)
		}
		err := m.host.stopInvocation(ctx, req.ID, req.InvocationID)
		if err != nil {
			if withdrawErr := m.host.withdrawCancel(ctx, unit, req.InvocationID); withdrawErr != nil {
				slog.Warn("could not withdraw the cancel request of the scheduled run", "unit", unit, "invocation_id", req.InvocationID, "error", withdrawErr)
			}
		}
		return err
	})
}
