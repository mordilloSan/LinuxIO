// Package systemd is the unit-control facade over the system D-Bus manager.
// It is shared by the bridge handlers, bridge internals and the linuxio CLI;
// the systemd.* route handlers live in backend/bridge/handlers/systemd.
package systemd

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
)

var managerIface = dbusclient.SystemdManager.Interface(dbusclient.SystemdManagerIface)

func requireUnitName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("unit name is required")
	}
	return nil
}

func StartUnit(ctx context.Context, name string) error {
	return callUnitJob(ctx, name, "StartUnit", "start")
}

func StopUnit(ctx context.Context, name string) error {
	return callUnitJob(ctx, name, "StopUnit", "stop")
}

func RestartUnit(ctx context.Context, name string) error {
	return callUnitJob(ctx, name, "RestartUnit", "restart")
}

func ReloadUnit(ctx context.Context, name string) error {
	return callUnitJob(ctx, name, "ReloadUnit", "reload")
}

// ReloadOrTryRestartUnit reloads a running unit and does nothing for a stopped
// one. A running unit without ExecReload is restarted instead.
func ReloadOrTryRestartUnit(ctx context.Context, name string) error {
	return callUnitJob(ctx, name, "ReloadOrTryRestartUnit", "try-reload-or-restart")
}

func EnableUnit(ctx context.Context, name string) error {
	return unitFilesOp(ctx, name, "EnableUnitFiles", "enable", []any{true}, new(bool))
}

func DisableUnit(ctx context.Context, name string) error {
	return unitFilesOp(ctx, name, "DisableUnitFiles", "disable", nil)
}

func MaskUnit(ctx context.Context, name string) error {
	return unitFilesOp(ctx, name, "MaskUnitFiles", "mask", []any{true})
}

func UnmaskUnit(ctx context.Context, name string) error {
	return unitFilesOp(ctx, name, "UnmaskUnitFiles", "unmask", nil)
}

func ResetFailedUnit(ctx context.Context, name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	if err := managerIface.Call(ctx, "ResetFailedUnit", dbusclient.CallPolicy{}, name); err != nil {
		return fmt.Errorf("reset failed unit %s: %w", name, err)
	}
	return nil
}

// ErrNoUnitFile is GetUnitFileState's error for a unit that no unit file
// defines, as one whose package is not installed.
var ErrNoUnitFile = errors.New("no unit file")

func GetUnitFileState(ctx context.Context, name string) (string, error) {
	if err := requireUnitName(name); err != nil {
		return "", err
	}
	var state string
	if err := managerIface.CallStore(ctx, "GetUnitFileState", dbusclient.CallPolicy{}, []any{name}, &state); err != nil {
		if noUnitFile(err) {
			// Both stay in the chain: callers that match the bus error by
			// name (btrfsTimerEnabled) keep working beside errors.Is.
			return "", fmt.Errorf("get unit file state %s: %w: %w", name, ErrNoUnitFile, err)
		}
		return "", fmt.Errorf("get unit file state %s: %w", name, err)
	}
	return state, nil
}

// noUnitFile reports whether err is the manager's answer for a unit no unit
// file defines: the bus error systemd maps ENOENT to.
func noUnitFile(err error) bool {
	var busErr godbus.Error
	return errors.As(err, &busErr) && busErr.Name == "org.freedesktop.DBus.Error.FileNotFound"
}

func DaemonReload(ctx context.Context) error {
	if err := managerIface.Call(ctx, "Reload", dbusclient.CallPolicy{}); err != nil {
		return fmt.Errorf("reload systemd manager: %w", err)
	}
	return nil
}

func GetActiveState(ctx context.Context, name string) (string, error) {
	if err := requireUnitName(name); err != nil {
		return "", err
	}
	var state string
	if err := dbusclient.SystemdManager.UseSession(ctx, func(session dbusclient.SystemSession) error {
		var path dbusclient.ObjectPath
		if err := session.CallStore(dbusclient.SystemdManagerIface+".GetUnit", dbusclient.CallPolicy{}, []any{name}, &path); err != nil {
			if session.Context().Err() != nil {
				return session.Context().Err()
			}
			// Unit not loaded means inactive for the callers that use this helper.
			state = "inactive"
			return nil
		}

		unit := session.ObjectAt(path)
		activeState, err := dbusclient.GetProperty[string](session, unit, dbusclient.SystemdUnitIface, "ActiveState")
		if err != nil {
			if session.Context().Err() != nil {
				return session.Context().Err()
			}
			state = "unknown"
			return nil
		}
		state = activeState
		return nil
	}); err != nil {
		return "", err
	}
	return state, nil
}

// GetInvocationID returns the unit's InvocationID (systemd 232 and later) as
// hex. It is "" for a unit that is not loaded or has never run. Each start of
// the unit gets a new one, so it tells a restarted unit from the old run.
func GetInvocationID(ctx context.Context, name string) (string, error) {
	if err := requireUnitName(name); err != nil {
		return "", err
	}
	var id string
	err := dbusclient.SystemdManager.UseSession(ctx, func(session dbusclient.SystemSession) error {
		var path dbusclient.ObjectPath
		if err := session.CallStore(dbusclient.SystemdManagerIface+".GetUnit", dbusclient.CallPolicy{}, []any{name}, &path); err != nil {
			if session.Context().Err() != nil {
				return session.Context().Err()
			}
			return nil
		}
		raw, err := dbusclient.GetProperty[[]byte](session, session.ObjectAt(path), dbusclient.SystemdUnitIface, "InvocationID")
		if err != nil {
			return fmt.Errorf("get invocation id %s: %w", name, err)
		}
		id = hex.EncodeToString(raw)
		return nil
	})
	return id, err
}

// UnitRunsInvocation reports whether unit's current invocation is
// invocationID and its ActiveState is activating, active, reloading or
// deactivating. A unit that is not loaded runs nothing. Any other failure to
// ask, such as a manager that is re-executing or does not answer, is an error,
// never false.
func UnitRunsInvocation(ctx context.Context, unit, invocationID string) (bool, error) {
	if err := requireUnitName(unit); err != nil {
		return false, err
	}
	runs := false
	err := dbusclient.SystemdManager.UseSession(ctx, func(session dbusclient.SystemSession) error {
		var path dbusclient.ObjectPath
		if err := session.CallStore(dbusclient.SystemdManagerIface+".GetUnit", dbusclient.CallPolicy{}, []any{unit}, &path); err != nil {
			if noSuchUnit(err) {
				return nil
			}
			return fmt.Errorf("get unit %s: %w", unit, err)
		}
		obj := session.ObjectAt(path)
		raw, err := dbusclient.GetProperty[[]byte](session, obj, dbusclient.SystemdUnitIface, "InvocationID")
		if err != nil {
			return fmt.Errorf("get invocation id %s: %w", unit, err)
		}
		if hex.EncodeToString(raw) != invocationID {
			return nil
		}
		state, err := dbusclient.GetProperty[string](session, obj, dbusclient.SystemdUnitIface, "ActiveState")
		if err != nil {
			return fmt.Errorf("get active state %s: %w", unit, err)
		}
		switch state {
		case "activating", "active", "reloading", "deactivating":
			runs = true
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return runs, nil
}

// UnitState is a unit as the manager knows it.
type UnitState struct {
	LoadState   string
	ActiveState string
	// SourcePath is the file a generator made the unit from, such as
	// /etc/fstab for the mount units of systemd-fstab-generator.
	SourcePath string
}

// GetUnitStates reads each of names, loading a unit the manager has not
// loaded, as one it collected after it stopped: a unit no file defines then
// reads LoadState "not-found". A name the manager refuses as no valid unit
// name, such as one longer than it allows, is left out.
func GetUnitStates(ctx context.Context, names []string) (map[string]UnitState, error) {
	units := map[string]UnitState{}
	err := dbusclient.SystemdManager.UseSession(ctx, func(session dbusclient.SystemSession) error {
		for _, name := range names {
			path, err := UnitObjectPath(session, name)
			if invalidArgs(err) {
				slog.Debug("systemd refused a unit name", "unit", name, "error", err)
				continue
			}
			if err != nil {
				return fmt.Errorf("load unit %s: %w", name, err)
			}
			obj := session.ObjectAt(path)
			var u UnitState
			for prop, dst := range map[string]*string{"LoadState": &u.LoadState, "ActiveState": &u.ActiveState, "SourcePath": &u.SourcePath} {
				if *dst, err = dbusclient.GetProperty[string](session, obj, dbusclient.SystemdUnitIface, prop); err != nil {
					return fmt.Errorf("get %s of %s: %w", prop, name, err)
				}
			}
			units[name] = u
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return units, nil
}

// invalidArgs reports whether err is the manager's answer for a unit name it
// does not accept (manager_load_unit_prepare).
func invalidArgs(err error) bool {
	var busErr godbus.Error
	return errors.As(err, &busErr) && busErr.Name == "org.freedesktop.DBus.Error.InvalidArgs"
}

// SocketState is a socket unit as the manager reports it.
type SocketState struct {
	// JobPending is true while a job is queued or running for the socket;
	// ActiveState and Result are not read then.
	JobPending  bool
	ActiveState string
	// Result is how the socket's last run ended, such as "success", or
	// "resources" when it could not open or watch its listening sockets (a
	// bind that found its address in use). It holds until the next start.
	Result string
}

// GetSocketState reads a loaded socket unit's job, ActiveState and Result.
func GetSocketState(ctx context.Context, name string) (SocketState, error) {
	if err := requireUnitName(name); err != nil {
		return SocketState{}, err
	}
	var state SocketState
	err := dbusclient.SystemdManager.UseSession(ctx, func(session dbusclient.SystemSession) error {
		var path dbusclient.ObjectPath
		if err := session.CallStore(dbusclient.SystemdManagerIface+".GetUnit", dbusclient.CallPolicy{}, []any{name}, &path); err != nil {
			return fmt.Errorf("get unit %s: %w", name, err)
		}
		obj := session.ObjectAt(path)
		job, err := dbusclient.GetProperty[[]any](session, obj, dbusclient.SystemdUnitIface, "Job")
		if err != nil {
			return fmt.Errorf("get job %s: %w", name, err)
		}
		var jobID uint32
		ok := false
		if len(job) == 2 {
			jobID, ok = job[0].(uint32)
		}
		if !ok {
			return fmt.Errorf("job of %s is %v, want an id and a path", name, job)
		}
		if jobID != 0 {
			state.JobPending = true
			return nil
		}
		if state.ActiveState, err = dbusclient.GetProperty[string](session, obj, dbusclient.SystemdUnitIface, "ActiveState"); err != nil {
			return fmt.Errorf("get active state %s: %w", name, err)
		}
		if state.Result, err = dbusclient.GetProperty[string](session, obj, dbusclient.SystemdSocketIface, "Result"); err != nil {
			return fmt.Errorf("get result %s: %w", name, err)
		}
		return nil
	})
	if err != nil {
		return SocketState{}, err
	}
	return state, nil
}

// noSuchUnit reports whether err is the manager's answer for a unit that is
// not loaded.
func noSuchUnit(err error) bool {
	var busErr godbus.Error
	return errors.As(err, &busErr) && busErr.Name == "org.freedesktop.systemd1.NoSuchUnit"
}

// SystemStopping reports whether the manager's SystemState is "stopping",
// which is how a host shutting down looks to a unit it is stopping. It is
// false when the state cannot be read.
func SystemStopping(ctx context.Context) bool {
	stopping := false
	err := dbusclient.SystemdManager.UseSession(ctx, func(session dbusclient.SystemSession) error {
		state, err := dbusclient.GetProperty[string](session, session.Object(), dbusclient.SystemdManagerIface, "SystemState")
		if err != nil {
			return err
		}
		stopping = state == "stopping"
		return nil
	})
	if err != nil {
		slog.Debug("could not read the system state", "error", err)
		return false
	}
	return stopping
}

// UnitObjectPath resolves a unit's object path, loading the unit when it is
// not loaded yet.
func UnitObjectPath(session dbusclient.SystemSession, unitName string) (dbusclient.ObjectPath, error) {
	var unitPath dbusclient.ObjectPath
	if err := session.CallStore(dbusclient.SystemdManagerIface+".GetUnit", dbusclient.CallPolicy{}, []any{unitName}, &unitPath); err == nil {
		return unitPath, nil
	}

	if err := session.CallStore(dbusclient.SystemdManagerIface+".LoadUnit", dbusclient.CallPolicy{}, []any{unitName}, &unitPath); err != nil {
		return "", err
	}

	return unitPath, nil
}

type UnitStatus struct {
	Name        string
	Description string
	LoadState   string
	ActiveState string
	SubState    string
}

func ListUnitsWithPrefix(ctx context.Context, prefix string) ([]UnitStatus, error) {
	var result [][]any
	if err := managerIface.CallStore(ctx, "ListUnits", dbusclient.CallPolicy{}, nil, &result); err != nil {
		return nil, fmt.Errorf("list units: %w", err)
	}

	units := make([]UnitStatus, 0)
	for _, unit := range result {
		if len(unit) < 5 {
			continue
		}
		name, ok := unit[0].(string)
		if !ok || !strings.HasPrefix(name, prefix) {
			continue
		}
		description, _ := unit[1].(string)
		loadState, _ := unit[2].(string)
		activeState, _ := unit[3].(string)
		subState, _ := unit[4].(string)
		units = append(units, UnitStatus{
			Name:        name,
			Description: description,
			LoadState:   loadState,
			ActiveState: activeState,
			SubState:    subState,
		})
	}
	return units, nil
}

func callUnitJob(ctx context.Context, name, method, operation string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	var job dbusclient.ObjectPath
	if err := managerIface.CallStore(ctx, method, dbusclient.CallPolicy{}, []any{name, "replace"}, &job); err != nil {
		return fmt.Errorf("%s unit %s: %w", operation, name, err)
	}
	return nil
}

// unitFilesOp runs a systemd *UnitFiles method for one unit, then reloads the
// manager. extraArgs follow the (names, runtime) pair; extraOut receives any
// results that precede the changes list.
func unitFilesOp(ctx context.Context, name, method, operation string, extraArgs []any, extraOut ...any) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	var changes [][]any
	args := append([]any{[]string{name}, false}, extraArgs...)
	if err := managerIface.CallStore(ctx, method, dbusclient.CallPolicy{}, args, append(extraOut, &changes)...); err != nil {
		return fmt.Errorf("%s unit files %s: %w", operation, name, err)
	}
	return DaemonReload(ctx)
}
