package systemd

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
)

type TimerStatus struct {
	UnitStatus
	NextElapseUSec  uint64 `json:"next_elapse_usec"`
	LastTriggerUSec uint64 `json:"last_trigger_usec"`
	Unit            string `json:"unit"`
}

func ListTimers(ctx context.Context) ([]TimerStatus, error) {
	var timers []TimerStatus
	err := dbusclient.SystemdManager.UseSession(ctx, func(session dbusclient.SystemSession) error {
		entries, err := listUnitsBySuffix(session, ".timer")
		if err != nil {
			return err
		}

		results := make([]TimerStatus, len(entries))
		if err := forEachListedUnitLimited(session.Context(), entries, func(i int, entry listedUnit) {
			results[i] = fetchTimerStatus(session, entry)
		}); err != nil {
			return err
		}
		timers = results
		return nil
	})
	return timers, err
}

func fetchTimerStatus(session dbusclient.SystemSession, entry listedUnit) TimerStatus {
	timer := TimerStatus{UnitStatus: entry.UnitStatus}
	if entry.Path == "" {
		return timer
	}

	unit := session.ObjectAt(entry.Path)
	timer.fetchUnitProperties(session, unit)
	if next, ok := timerNextElapseUsec(session, unit); ok {
		timer.NextElapseUSec = next
	}
	if last, ok := getUint64Property(session, unit, dbusclient.SystemdTimerIface, "LastTriggerUSec"); ok {
		timer.LastTriggerUSec = last
	}
	if target, ok := getStringProperty(session, unit, dbusclient.SystemdTimerIface, "Unit"); ok {
		timer.Unit = target
	}
	return timer
}
