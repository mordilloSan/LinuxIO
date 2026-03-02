package dbus

import (
	"fmt"
	"strings"
	"sync"

	godbus "github.com/godbus/dbus/v5"
	"github.com/mordilloSan/go-logger/logger"

	systemdapi "github.com/mordilloSan/LinuxIO/backend/common/systemd"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

type ServiceStatus struct {
	Name                   string `json:"name"`
	Description            string `json:"description"`
	LoadState              string `json:"load_state"`
	ActiveState            string `json:"active_state"`
	SubState               string `json:"sub_state"`
	UnitFileState          string `json:"unit_file_state"`
	ActiveEnterTimestamp   uint64 `json:"active_enter_timestamp"`
	InactiveEnterTimestamp uint64 `json:"inactive_enter_timestamp"`
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
		var units [][]any
		if err := systemd.Call("org.freedesktop.systemd1.Manager.ListUnits", 0).Store(&units); err != nil {
			return err
		}

		// First pass: extract basic fields and unit object paths from ListUnits
		type unitEntry struct {
			svc  ServiceStatus
			path godbus.ObjectPath
		}
		var entries []unitEntry
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
			unitPath, _ := u[6].(godbus.ObjectPath)
			entries = append(entries, unitEntry{
				svc: ServiceStatus{
					Name:        name,
					Description: desc,
					LoadState:   loadState,
					ActiveState: activeState,
					SubState:    subState,
				},
				path: unitPath,
			})
		}

		// Second pass: fetch extra properties in parallel, one goroutine per unit
		results := make([]ServiceStatus, len(entries))
		var wg sync.WaitGroup
		for i, e := range entries {
			wg.Add(1)
			go func(i int, e unitEntry) {
				defer wg.Done()
				svc := e.svc
				if e.path != "" {
					unit := conn.Object("org.freedesktop.systemd1", e.path)
					if val, err := unit.GetProperty("org.freedesktop.systemd1.Unit.UnitFileState"); err == nil {
						if s, ok := val.Value().(string); ok {
							svc.UnitFileState = s
						}
					}
					if val, err := unit.GetProperty("org.freedesktop.systemd1.Unit.ActiveEnterTimestamp"); err == nil {
						if t, ok := val.Value().(uint64); ok {
							svc.ActiveEnterTimestamp = t
						}
					}
					if val, err := unit.GetProperty("org.freedesktop.systemd1.Unit.InactiveEnterTimestamp"); err == nil {
						if t, ok := val.Value().(uint64); ok {
							svc.InactiveEnterTimestamp = t
						}
					}
				}
				results[i] = svc
			}(i, e)
		}
		wg.Wait()
		services = results
		return nil
	})
	return services, err
}

// --- Get detailed info about a single service (robust) ---
func GetServiceInfo(serviceName string) (map[string]any, error) {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		err := fmt.Errorf("missing service name")
		logger.Errorf(" GetServiceInfo failed: %v", err)
		return nil, err
	}

	var info map[string]any
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
			"Requires", "Wants", "WantedBy", "Before", "After",
			"Conflicts", "PartOf", "TriggeredBy", "MemoryCurrent",
		}
		info = make(map[string]any)
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
	return systemdapi.StartUnit(name)
}

// Stop a service
func StopService(name string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return systemdapi.StopUnit(name)
}

// Restart a service
func RestartService(name string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return systemdapi.RestartUnit(name)
}

// Reload a service (if supported)
func ReloadService(name string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return systemdapi.ReloadUnit(name)
}

// Enable a service (for boot)
func EnableService(name string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return systemdapi.EnableUnit(name)
}

// Disable a service (prevent start at boot)
func DisableService(name string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return systemdapi.DisableUnit(name)
}

// Mask a service (make it unstartable even manually)
func MaskService(name string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return systemdapi.MaskUnit(name)
}

// Unmask a service
func UnmaskService(name string) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()
	return systemdapi.UnmaskUnit(name)
}
