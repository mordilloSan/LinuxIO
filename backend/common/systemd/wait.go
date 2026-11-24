package systemd

import (
	"context"
	"fmt"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
)

// unitJobPoll is how often a waiting call looks at the unit's job.
const unitJobPoll = 100 * time.Millisecond

// StartUnitAndWait starts the unit and returns when its start job is over:
// nil when the unit is then active, and an error naming its state otherwise,
// as for a oneshot whose command failed. It is for a unit that stays active
// once started (a service, or a oneshot with RemainAfterExit=yes): a plain
// oneshot is inactive after a successful run and would read as failed. The
// job is the manager's: when ctx ends first the start carries on, and a
// caller that must not leave the unit started stops it.
func StartUnitAndWait(ctx context.Context, name string) error {
	return runUnitJob(ctx, name, "StartUnit", "start", "active")
}

// StopUnitAndWait stops the unit and returns when its stop job is over: nil
// when the unit is then inactive, and an error naming its state otherwise, as
// "failed" for a unit whose stop command failed.
func StopUnitAndWait(ctx context.Context, name string) error {
	return runUnitJob(ctx, name, "StopUnit", "stop", "inactive")
}

// runUnitJob queues the job and waits until the unit no longer carries it,
// then judges the unit by its state: the job's own result is only sent as a
// signal, and the state says the same for a start or a stop. A job another
// one replaced counts as over, and the state then tells how the unit ended.
func runUnitJob(ctx context.Context, name, method, operation, want string) error {
	job, err := queueUnitJob(ctx, name, method, operation)
	if err != nil {
		return err
	}
	for {
		state, pending, err := unitJobState(ctx, name, job)
		if err != nil {
			return fmt.Errorf("%s unit %s: %w", operation, name, err)
		}
		if !pending {
			if state == want {
				return nil
			}
			return fmt.Errorf("%s unit %s: the unit is %s", operation, name, state)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s unit %s: %w", operation, name, ctx.Err())
		case <-time.After(unitJobPoll):
		}
	}
}

// unitJobState reports whether the unit still carries job, and the unit's
// ActiveState, read after the job so that a job seen gone has a final state.
func unitJobState(ctx context.Context, name string, job dbusclient.ObjectPath) (state string, pending bool, err error) {
	err = dbusclient.SystemdManager.UseSession(ctx, func(session dbusclient.SystemSession) error {
		path, loadErr := UnitObjectPath(session, name)
		if loadErr != nil {
			return fmt.Errorf("load unit: %w", loadErr)
		}
		unit := session.ObjectAt(path)
		// Job is (id, object path), and (0, "/") for a unit with no job.
		current, readErr := dbusclient.GetProperty[[]any](session, unit, dbusclient.SystemdUnitIface, "Job")
		if readErr != nil {
			return readErr
		}
		if len(current) == 2 {
			currentPath, _ := current[1].(dbusclient.ObjectPath)
			pending = currentPath == job
		}
		state, readErr = dbusclient.GetProperty[string](session, unit, dbusclient.SystemdUnitIface, "ActiveState")
		return readErr
	})
	return state, pending, err
}
