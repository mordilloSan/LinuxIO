package dbus

import (
	"fmt"
	"go-backend/internal/logger"
	"strings"

	"github.com/godbus/dbus/v5"
)

type ServiceStatus struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	LoadState   string `json:"load_state"`
	ActiveState string `json:"active_state"`
	SubState    string `json:"sub_state"`
}

// --- List all services (robust) ---
func ListServices() ([]ServiceStatus, error) {
	var services []ServiceStatus
	err := RetryOnceIfClosed(nil, func() error {
		conn, err := dbus.SystemBus()
		if err != nil {
			return err
		}
		defer conn.Close()

		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		var units [][]interface{}
		if err := systemd.Call("org.freedesktop.systemd1.Manager.ListUnits", 0).Store(&units); err != nil {
			return err
		}

		for _, u := range units {
			name := u[0].(string)
			if !strings.HasSuffix(name, ".service") {
				continue
			}
			svc := ServiceStatus{
				Name:        name,
				Description: u[1].(string),
				LoadState:   u[2].(string),
				ActiveState: u[3].(string),
				SubState:    u[4].(string),
			}
			services = append(services, svc)
		}
		return nil
	})
	return services, err
}

// --- Get detailed info about a single service (robust) ---
func GetServiceInfo(serviceName string) (map[string]interface{}, error) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		err := fmt.Errorf("missing service name")
		logger.Errorf("❌ GetServiceInfo failed: %v", err)
		return nil, err
	}

	var info map[string]interface{}
	err := RetryOnceIfClosed(nil, func() error {
		conn, err := dbus.SystemBus()
		if err != nil {
			return err
		}
		defer conn.Close()

		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		var unitPath dbus.ObjectPath
		if err := systemd.Call("org.freedesktop.systemd1.Manager.GetUnit", 0, serviceName).Store(&unitPath); err != nil {
			return err
		}
		unit := conn.Object("org.freedesktop.systemd1", unitPath)

		props := []string{
			"Id", "Description", "LoadState", "ActiveState", "SubState",
			"UnitFileState", "FragmentPath", "ActiveEnterTimestamp", "InactiveEnterTimestamp",
		}
		info = make(map[string]interface{})
		for _, prop := range props {
			val, err := unit.GetProperty("org.freedesktop.systemd1.Unit." + prop)
			if err == nil {
				info[prop] = val.Value()
			}
		}
		if val, err := unit.GetProperty("org.freedesktop.systemd1.Service.MainPID"); err == nil {
			info["MainPID"] = val.Value()
		}
		if val, err := unit.GetProperty("org.freedesktop.systemd1.Service.ExecMainStatus"); err == nil {
			info["ExecMainStatus"] = val.Value()
		}
		return nil
	})
	return info, err
}

// Start a service
func StartService(name string) error {
	return RetryOnceIfClosed(nil, func() error {
		conn, err := dbus.SystemBus()
		if err != nil {
			return err
		}
		defer conn.Close()
		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		// "replace" is the mode systemctl uses by default
		call := systemd.Call("org.freedesktop.systemd1.Manager.StartUnit", 0, name, "replace")
		return call.Err
	})
}

// Stop a service
func StopService(name string) error {
	return RetryOnceIfClosed(nil, func() error {
		conn, err := dbus.SystemBus()
		if err != nil {
			return err
		}
		defer conn.Close()
		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		call := systemd.Call("org.freedesktop.systemd1.Manager.StopUnit", 0, name, "replace")
		return call.Err
	})
}

// Restart a service
func RestartService(name string) error {
	return RetryOnceIfClosed(nil, func() error {
		conn, err := dbus.SystemBus()
		if err != nil {
			return err
		}
		defer conn.Close()
		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		call := systemd.Call("org.freedesktop.systemd1.Manager.RestartUnit", 0, name, "replace")
		return call.Err
	})
}

// Reload a service (if supported)
func ReloadService(name string) error {
	return RetryOnceIfClosed(nil, func() error {
		conn, err := dbus.SystemBus()
		if err != nil {
			return err
		}
		defer conn.Close()
		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		call := systemd.Call("org.freedesktop.systemd1.Manager.ReloadUnit", 0, name, "replace")
		return call.Err
	})
}

// Enable a service (for boot)
func EnableService(name string) error {
	return RetryOnceIfClosed(nil, func() error {
		conn, err := dbus.SystemBus()
		if err != nil {
			return err
		}
		defer conn.Close()
		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		// changes := ...  // <-- REMOVE THIS
		call := systemd.Call("org.freedesktop.systemd1.Manager.EnableUnitFiles", 0, []string{name}, false, true)
		return call.Err
	})
}

// Disable a service (prevent start at boot)
func DisableService(name string) error {
	return RetryOnceIfClosed(nil, func() error {
		conn, err := dbus.SystemBus()
		if err != nil {
			return err
		}
		defer conn.Close()
		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		// changes := ...  // <-- REMOVE THIS
		call := systemd.Call("org.freedesktop.systemd1.Manager.DisableUnitFiles", 0, []string{name}, false)
		return call.Err
	})
}

// Mask a service (make it unstartable even manually)
func MaskService(name string) error {
	return RetryOnceIfClosed(nil, func() error {
		conn, err := dbus.SystemBus()
		if err != nil {
			return err
		}
		defer conn.Close()
		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		// changes := ...  // <-- REMOVE THIS
		call := systemd.Call("org.freedesktop.systemd1.Manager.MaskUnitFiles", 0, []string{name}, false, true)
		return call.Err
	})
}

// Unmask a service
func UnmaskService(name string) error {
	return RetryOnceIfClosed(nil, func() error {
		conn, err := dbus.SystemBus()
		if err != nil {
			return err
		}
		defer conn.Close()
		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		// changes := ...  // <-- REMOVE THIS
		call := systemd.Call("org.freedesktop.systemd1.Manager.UnmaskUnitFiles", 0, []string{name}, false)
		return call.Err
	})
}
