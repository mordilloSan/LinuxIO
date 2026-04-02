package systemd

import (
	"fmt"
	"strings"

	godbus "github.com/godbus/dbus/v5"
)

const (
	systemdBusName    = "org.freedesktop.systemd1"
	systemdObjectPath = "/org/freedesktop/systemd1"
	systemdMgrIface   = "org.freedesktop.systemd1.Manager"
)

func withManager(call func(manager godbus.BusObject) error) error {
	conn, err := godbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("connect system bus: %w", err)
	}
	defer conn.Close()

	manager := conn.Object(systemdBusName, systemdObjectPath)
	return call(manager)
}

func requireUnitName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("unit name is required")
	}
	return nil
}

func StartUnit(name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	return withManager(func(manager godbus.BusObject) error {
		if err := manager.Call(systemdMgrIface+".StartUnit", 0, name, "replace").Err; err != nil {
			return fmt.Errorf("start unit %s: %w", name, err)
		}
		return nil
	})
}

func StopUnit(name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	return withManager(func(manager godbus.BusObject) error {
		if err := manager.Call(systemdMgrIface+".StopUnit", 0, name, "replace").Err; err != nil {
			return fmt.Errorf("stop unit %s: %w", name, err)
		}
		return nil
	})
}

func RestartUnit(name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	return withManager(func(manager godbus.BusObject) error {
		if err := manager.Call(systemdMgrIface+".RestartUnit", 0, name, "replace").Err; err != nil {
			return fmt.Errorf("restart unit %s: %w", name, err)
		}
		return nil
	})
}

func ReloadUnit(name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	return withManager(func(manager godbus.BusObject) error {
		if err := manager.Call(systemdMgrIface+".ReloadUnit", 0, name, "replace").Err; err != nil {
			return fmt.Errorf("reload unit %s: %w", name, err)
		}
		return nil
	})
}

func EnableUnit(name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	return withManager(func(manager godbus.BusObject) error {
		if err := manager.Call(systemdMgrIface+".EnableUnitFiles", 0, []string{name}, false, true).Err; err != nil {
			return fmt.Errorf("enable unit file %s: %w", name, err)
		}
		return nil
	})
}

func DisableUnit(name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	return withManager(func(manager godbus.BusObject) error {
		if err := manager.Call(systemdMgrIface+".DisableUnitFiles", 0, []string{name}, false).Err; err != nil {
			return fmt.Errorf("disable unit file %s: %w", name, err)
		}
		return nil
	})
}

func MaskUnit(name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	return withManager(func(manager godbus.BusObject) error {
		if err := manager.Call(systemdMgrIface+".MaskUnitFiles", 0, []string{name}, false, true).Err; err != nil {
			return fmt.Errorf("mask unit file %s: %w", name, err)
		}
		return nil
	})
}

func UnmaskUnit(name string) error {
	if err := requireUnitName(name); err != nil {
		return err
	}
	return withManager(func(manager godbus.BusObject) error {
		if err := manager.Call(systemdMgrIface+".UnmaskUnitFiles", 0, []string{name}, false).Err; err != nil {
			return fmt.Errorf("unmask unit file %s: %w", name, err)
		}
		return nil
	})
}

func GetUnitFileState(name string) (string, error) {
	if err := requireUnitName(name); err != nil {
		return "", err
	}
	var state string
	if err := withManager(func(manager godbus.BusObject) error {
		if err := manager.Call(systemdMgrIface+".GetUnitFileState", 0, name).Store(&state); err != nil {
			return fmt.Errorf("get unit file state %s: %w", name, err)
		}
		return nil
	}); err != nil {
		return "", err
	}
	return state, nil
}

func DaemonReload() error {
	return withManager(func(manager godbus.BusObject) error {
		if err := manager.Call(systemdMgrIface+".Reload", 0).Err; err != nil {
			return fmt.Errorf("daemon reload: %w", err)
		}
		return nil
	})
}

func GetActiveState(name string) (string, error) {
	if err := requireUnitName(name); err != nil {
		return "", err
	}
	conn, err := godbus.ConnectSystemBus()
	if err != nil {
		return "", fmt.Errorf("connect system bus: %w", err)
	}
	defer conn.Close()

	manager := conn.Object(systemdBusName, systemdObjectPath)
	var path godbus.ObjectPath
	if err := manager.Call(systemdMgrIface+".GetUnit", 0, name).Store(&path); err != nil {
		// Unit not loaded → treat as inactive
		return "inactive", nil
	}
	unit := conn.Object(systemdBusName, path)
	prop, err := unit.GetProperty("org.freedesktop.systemd1.Unit.ActiveState")
	if err != nil {
		return "unknown", nil
	}
	s, ok := prop.Value().(string)
	if !ok {
		return "unknown", nil
	}
	return s, nil
}

type UnitStatus struct {
	Name        string
	Description string
	LoadState   string
	ActiveState string
	SubState    string
}

func ListUnitsWithPrefix(prefix string) ([]UnitStatus, error) {
	var units []UnitStatus
	if err := withManager(func(manager godbus.BusObject) error {
		var result [][]any
		if err := manager.Call(systemdMgrIface+".ListUnits", 0).Store(&result); err != nil {
			return fmt.Errorf("list units: %w", err)
		}
		for _, u := range result {
			name, ok := u[0].(string)
			if !ok || !strings.HasPrefix(name, prefix) {
				continue
			}
			description, _ := u[1].(string)
			loadState, _ := u[2].(string)
			activeState, _ := u[3].(string)
			subState, _ := u[4].(string)
			units = append(units, UnitStatus{
				Name:        name,
				Description: description,
				LoadState:   loadState,
				ActiveState: activeState,
				SubState:    subState,
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return units, nil
}
