package systemd

import (
	"context"
	"fmt"
	"strings"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
)

var managerIface = dbusclient.SystemdManager.Interface(dbusclient.SystemdManagerIface)

func requireUnitName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("unit name is required")
	}
	return nil
}

func StartUnit(ctx context.Context, name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	return callUnitJob(ctx, name, "StartUnit", "start")
}

func StopUnit(ctx context.Context, name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	return callUnitJob(ctx, name, "StopUnit", "stop")
}

func RestartUnit(ctx context.Context, name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	return callUnitJob(ctx, name, "RestartUnit", "restart")
}

func ReloadUnit(ctx context.Context, name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	return callUnitJob(ctx, name, "ReloadUnit", "reload")
}

func EnableUnit(ctx context.Context, name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	names := []string{name}
	if err := enableUnitFiles(ctx, names); err != nil {
		return err
	}
	return DaemonReload(ctx)
}

func DisableUnit(ctx context.Context, name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	names := []string{name}
	if err := disableUnitFiles(ctx, names); err != nil {
		return err
	}
	return DaemonReload(ctx)
}

func MaskUnit(ctx context.Context, name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	names := []string{name}
	if err := maskUnitFiles(ctx, names); err != nil {
		return err
	}
	return DaemonReload(ctx)
}

func UnmaskUnit(ctx context.Context, name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	names := []string{name}
	if err := unmaskUnitFiles(ctx, names); err != nil {
		return err
	}
	return DaemonReload(ctx)
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
		return "", err
	}
	return state, nil
}

func DaemonReload(ctx context.Context) error {
	return reloadManager(ctx)
}

func GetActiveState(ctx context.Context, name string) (string, error) {
	if err := requireUnitName(name); err != nil {
		return "", err
	}
	var state string
	if err := dbusclient.SystemdManager.UseSession(ctx, func(session dbusclient.SystemSession) error {
		var path godbus.ObjectPath
		if err := session.CallStore(dbusclient.SystemdManagerIface+".GetUnit", dbusclient.CallPolicy{}, []any{name}, &path); err != nil {
			if session.Context().Err() != nil {
				return session.Context().Err()
			}
			// Unit not loaded means inactive for the callers that use this helper.
			state = "inactive"
			return nil
		}

		unit := session.ObjectAt(path)
		activeState, err := dbusclient.GetProperty[string](session.Context(), unit, dbusclient.SystemdUnitIface, "ActiveState")
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
	var job godbus.ObjectPath
	if err := managerIface.CallStore(ctx, method, dbusclient.CallPolicy{}, []any{name, "replace"}, &job); err != nil {
		return fmt.Errorf("%s unit %s: %w", operation, name, err)
	}
	return nil
}

func enableUnitFiles(ctx context.Context, names []string) error {
	var carriesInstallInfo bool
	var changes [][]any
	if err := managerIface.CallStore(
		ctx,
		"EnableUnitFiles",
		dbusclient.CallPolicy{},
		[]any{names, false, true},
		&carriesInstallInfo,
		&changes,
	); err != nil {
		return fmt.Errorf("enable unit files %s: %w", strings.Join(names, ", "), err)
	}
	return nil
}

func disableUnitFiles(ctx context.Context, names []string) error {
	var changes [][]any
	if err := managerIface.CallStore(
		ctx,
		"DisableUnitFiles",
		dbusclient.CallPolicy{},
		[]any{names, false},
		&changes,
	); err != nil {
		return fmt.Errorf("disable unit files %s: %w", strings.Join(names, ", "), err)
	}
	return nil
}

func maskUnitFiles(ctx context.Context, names []string) error {
	var changes [][]any
	if err := managerIface.CallStore(
		ctx,
		"MaskUnitFiles",
		dbusclient.CallPolicy{},
		[]any{names, false, true},
		&changes,
	); err != nil {
		return fmt.Errorf("mask unit files %s: %w", strings.Join(names, ", "), err)
	}
	return nil
}

func unmaskUnitFiles(ctx context.Context, names []string) error {
	var changes [][]any
	if err := managerIface.CallStore(
		ctx,
		"UnmaskUnitFiles",
		dbusclient.CallPolicy{},
		[]any{names, false},
		&changes,
	); err != nil {
		return fmt.Errorf("unmask unit files %s: %w", strings.Join(names, ", "), err)
	}
	return nil
}

func reloadManager(ctx context.Context) error {
	if err := managerIface.Call(ctx, "Reload", dbusclient.CallPolicy{}); err != nil {
		return fmt.Errorf("reload systemd manager: %w", err)
	}
	return nil
}
