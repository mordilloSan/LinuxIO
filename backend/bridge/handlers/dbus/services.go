package dbus

import (
	"fmt"
	"os/exec"
	"strings"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/common/logger"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
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
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	var services []ServiceStatus
	err := RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.SystemBus()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil {
				logger.Warnf("failed to close D-Bus connection: %v", cerr)
			}
		}()

		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		var units [][]interface{}
		if err := systemd.Call("org.freedesktop.systemd1.Manager.ListUnits", 0).Store(&units); err != nil {
			return err
		}

		for _, u := range units {
			name, err := utils.AsString(u[0])
			if err != nil {
				return fmt.Errorf("invalid service name: %w", err)
			}
			if !strings.HasSuffix(name, ".service") {
				continue
			}

			desc, err := utils.AsString(u[1])
			if err != nil {
				return fmt.Errorf("invalid service description for %q: %w", name, err)
			}

			loadState, err := utils.AsString(u[2])
			if err != nil {
				return fmt.Errorf("invalid load state for %q: %w", name, err)
			}

			activeState, err := utils.AsString(u[3])
			if err != nil {
				return fmt.Errorf("invalid active state for %q: %w", name, err)
			}

			subState, err := utils.AsString(u[4])
			if err != nil {
				return fmt.Errorf("invalid substate for %q: %w", name, err)
			}

			svc := ServiceStatus{
				Name:        name,
				Description: desc,
				LoadState:   loadState,
				ActiveState: activeState,
				SubState:    subState,
			}
			services = append(services, svc)
		}
		return nil
	})
	return services, err
}

// --- Get detailed info about a single service (robust) ---
func GetServiceInfo(serviceName string) (map[string]interface{}, error) {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		err := fmt.Errorf("missing service name")
		logger.Errorf(" GetServiceInfo failed: %v", err)
		return nil, err
	}

	var info map[string]interface{}
	err := RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.SystemBus()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil {
				logger.Warnf("failed to close D-Bus connection: %v", cerr)
			}
		}()

		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		var unitPath godbus.ObjectPath
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
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.SystemBus()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil {
				logger.Warnf("failed to close D-Bus connection: %v", cerr)
			}
		}()

		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		// "replace" is the mode systemctl uses by default
		call := systemd.Call("org.freedesktop.systemd1.Manager.StartUnit", 0, name, "replace")
		return call.Err
	})
}

// Stop a service
func StopService(name string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.SystemBus()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil {
				logger.Warnf("failed to close D-Bus connection: %v", cerr)
			}
		}()

		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		call := systemd.Call("org.freedesktop.systemd1.Manager.StopUnit", 0, name, "replace")
		return call.Err
	})
}

// Restart a service
func RestartService(name string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.SystemBus()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil {
				logger.Warnf("failed to close D-Bus connection: %v", cerr)
			}
		}()

		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		call := systemd.Call("org.freedesktop.systemd1.Manager.RestartUnit", 0, name, "replace")
		return call.Err
	})
}

// Reload a service (if supported)
func ReloadService(name string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.SystemBus()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil {
				logger.Warnf("failed to close D-Bus connection: %v", cerr)
			}
		}()

		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		call := systemd.Call("org.freedesktop.systemd1.Manager.ReloadUnit", 0, name, "replace")
		return call.Err
	})
}

// Enable a service (for boot)
func EnableService(name string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.SystemBus()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil {
				logger.Warnf("failed to close D-Bus connection: %v", cerr)
			}
		}()

		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")

		call := systemd.Call("org.freedesktop.systemd1.Manager.EnableUnitFiles", 0, []string{name}, false, true)
		return call.Err
	})
}

// Disable a service (prevent start at boot)
func DisableService(name string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.SystemBus()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil {
				logger.Warnf("failed to close D-Bus connection: %v", cerr)
			}
		}()

		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		call := systemd.Call("org.freedesktop.systemd1.Manager.DisableUnitFiles", 0, []string{name}, false)
		return call.Err
	})
}

// Mask a service (make it unstartable even manually)
func MaskService(name string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.SystemBus()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil {
				logger.Warnf("failed to close D-Bus connection: %v", cerr)
			}
		}()

		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		call := systemd.Call("org.freedesktop.systemd1.Manager.MaskUnitFiles", 0, []string{name}, false, true)
		return call.Err
	})
}

// Unmask a service
func UnmaskService(name string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.SystemBus()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil {
				logger.Warnf("failed to close D-Bus connection: %v", cerr)
			}
		}()

		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
		call := systemd.Call("org.freedesktop.systemd1.Manager.UnmaskUnitFiles", 0, []string{name}, false)
		return call.Err
	})
}

// GetServiceLogs fetches logs for a service using journalctl
func GetServiceLogs(serviceName string, lines string) ([]string, error) {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return nil, fmt.Errorf("missing service name")
	}

	// Use journalctl to get logs
	cmd := exec.Command("journalctl", "-u", serviceName, "-n", lines, "--no-pager")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get logs: %v - %s", err, string(output))
	}

	// Split into lines
	logLines := strings.Split(string(output), "\n")
	return logLines, nil
}
