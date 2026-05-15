package systemd

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
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
	MainPID                int32  `json:"main_pid"`
}

// --- List all services (robust) ---
func ListServices(ctx context.Context) ([]ServiceStatus, error) {
	var services []ServiceStatus
	err := dbusclient.SystemdManager.UseSession(ctx, func(session dbusclient.SystemSession) error {
		entries, err := listUnitsBySuffix(session, ".service")
		if err != nil {
			return err
		}

		results := make([]ServiceStatus, len(entries))
		if err := forEachListedUnitLimited(session.Context(), entries, func(i int, entry listedUnit) {
			results[i] = fetchServiceStatus(session, entry)
		}); err != nil {
			return err
		}
		services = results
		return nil
	})
	return services, err
}

func fetchServiceStatus(session dbusclient.SystemSession, entry listedUnit) ServiceStatus {
	service := ServiceStatus{
		Name:          entry.Name,
		Description:   entry.Description,
		LoadState:     entry.LoadState,
		ActiveState:   entry.ActiveState,
		SubState:      entry.SubState,
		UnitFileState: entry.UnitFileState,
	}
	if entry.Path == "" {
		return service
	}

	unit := session.ObjectAt(entry.Path)
	if state, ok := getStringProperty(session, unit, dbusclient.SystemdUnitIface, "UnitFileState"); ok {
		service.UnitFileState = state
	}
	if ts, ok := getUint64Property(session, unit, dbusclient.SystemdUnitIface, "ActiveEnterTimestamp"); ok {
		service.ActiveEnterTimestamp = ts
	}
	if ts, ok := getUint64Property(session, unit, dbusclient.SystemdUnitIface, "InactiveEnterTimestamp"); ok {
		service.InactiveEnterTimestamp = ts
	}
	if pid, ok := getUint32Property(session, unit, dbusclient.SystemdServiceIface, "MainPID"); ok {
		service.MainPID = int32(pid)
	}
	return service
}
