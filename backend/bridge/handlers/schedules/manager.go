package schedules

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"
	"uuid"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
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
	for _, def := range defs {
		result.Schedules = append(result.Schedules, m.status(ctx, def))
	}
	return result, nil
}

func (m manager) get(ctx context.Context, id string) (apischema.ScheduleStatus, error) {
	def, err := m.store.definition(id)
	if err != nil {
		return apischema.ScheduleStatus{}, err
	}
	return m.status(ctx, def), nil
}

func (m manager) create(ctx context.Context, req apischema.ScheduleCreateRequest) (apischema.ScheduleStatus, error) {
	if err := m.host.available(); err != nil {
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
		return m.apply(ctx, def, account)
	})
	if err != nil {
		return apischema.ScheduleStatus{}, err
	}
	return m.status(ctx, def), nil
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
	if err := utils.WriteFileAtomic(m.store.scriptPath(def), []byte(def.Options.Script), 0o440, os.Geteuid(), gid); err != nil {
		return fmt.Errorf("save managed script: %w", err)
	}
	if err := utils.WriteFileAtomic(filepath.Join(m.store.unitDir, serviceName(def.ID)), m.store.renderService(def, account), 0o644); err != nil {
		return err
	}
	if err := utils.WriteFileAtomic(filepath.Join(m.store.unitDir, timerName(def.ID)), renderTimer(def), 0o644); err != nil {
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
		return m.changeLocked(ctx, id, options, enabled, deleting)
	})
}

func (m manager) changeLocked(ctx context.Context, id string, options *apischema.ScheduleOptions, enabled *bool, deleting bool) error {
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
	return removeFile(path)
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
		unit, err := os.ReadFile(filepath.Join(m.store.unitDir, serviceName(id)))
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
		// Reload repairs a previously interrupted reload; StartUnit is deliberately
		// not retried because a lost reply could otherwise execute a short job twice.
		if err := m.host.reload(ctx); err != nil {
			return err
		}
		return m.host.start(ctx, serviceName(id))
	})
}

func (m manager) stop(ctx context.Context, req apischema.ScheduleStopRequest) error {
	if !validInvocationID(req.InvocationID) {
		return errors.New("invalid systemd invocation ID")
	}
	return m.store.locked(ctx, func() error {
		if _, err := m.store.definition(req.ID); err != nil {
			return err
		}
		return m.host.stopInvocation(ctx, req.ID, req.InvocationID)
	})
}
