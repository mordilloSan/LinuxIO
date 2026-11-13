package systemd

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
)

type ServiceStatus struct {
	UnitStatus
	MainPID int32 `json:"main_pid"`
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

// ListServiceStates lists services with the name, description and states
// that ListUnits already returns, skipping the per-unit property reads.
func ListServiceStates(ctx context.Context) ([]ServiceStatus, error) {
	var services []ServiceStatus
	err := dbusclient.SystemdManager.UseSession(ctx, func(session dbusclient.SystemSession) error {
		entries, err := listUnitsBySuffix(session, ".service")
		if err != nil {
			return err
		}
		services = make([]ServiceStatus, len(entries))
		for i, entry := range entries {
			services[i] = ServiceStatus{UnitStatus: entry.UnitStatus}
		}
		return nil
	})
	return services, err
}

func fetchServiceStatus(session dbusclient.SystemSession, entry listedUnit) ServiceStatus {
	service := ServiceStatus{UnitStatus: entry.UnitStatus}
	if entry.Path == "" {
		return service
	}

	unit := session.ObjectAt(entry.Path)
	service.fetchUnitProperties(session, unit)
	if pid, ok := getUint32Property(session, unit, dbusclient.SystemdServiceIface, "MainPID"); ok {
		service.MainPID = int32(pid)
	}
	return service
}
