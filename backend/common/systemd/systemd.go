// Package systemd is the unit-control facade over the system D-Bus manager.
// It is shared by the bridge handlers, bridge internals and the linuxio CLI;
// the systemd.* route handlers live in backend/bridge/handlers/systemd.
package systemd

import (
	"context"
	"fmt"
	"strings"

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

func GetUnitFileState(ctx context.Context, name string) (string, error) {
	if err := requireUnitName(name); err != nil {
		return "", err
	}
	var state string
	if err := managerIface.CallStore(ctx, "GetUnitFileState", dbusclient.CallPolicy{}, []any{name}, &state); err != nil {
		return "", fmt.Errorf("get unit file state %s: %w", name, err)
	}
	return state, nil
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
