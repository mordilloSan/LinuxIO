package dbus

import (
	"sync"

	godbus "github.com/godbus/dbus/v5"

	systemdapi "github.com/mordilloSan/LinuxIO/backend/common/systemd"
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
	var services []ServiceStatus
	err := withSystemdManager(func(conn *godbus.Conn, systemd godbus.BusObject) error {
		entries, err := listUnitsBySuffix(systemd, ".service")
		if err != nil {
			return err
		}

		results := make([]ServiceStatus, len(entries))
		var wg sync.WaitGroup
		for i, entry := range entries {
			wg.Add(1)
			go func(i int, entry listedUnit) {
				defer wg.Done()
				service := ServiceStatus{
					Name:          entry.Name,
					Description:   entry.Description,
					LoadState:     entry.LoadState,
					ActiveState:   entry.ActiveState,
					SubState:      entry.SubState,
					UnitFileState: entry.UnitFileState,
				}

				if entry.Path != "" {
					unit := unitObject(conn, entry.Path)
					if state, ok := getStringProperty(unit, "org.freedesktop.systemd1.Unit.UnitFileState"); ok {
						service.UnitFileState = state
					}
					if ts, ok := getUint64Property(unit, "org.freedesktop.systemd1.Unit.ActiveEnterTimestamp"); ok {
						service.ActiveEnterTimestamp = ts
					}
					if ts, ok := getUint64Property(unit, "org.freedesktop.systemd1.Unit.InactiveEnterTimestamp"); ok {
						service.InactiveEnterTimestamp = ts
					}
				}

				results[i] = service
			}(i, entry)
		}

		wg.Wait()
		services = results
		return nil
	})
	return services, err
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
