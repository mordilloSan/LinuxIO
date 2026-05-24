package systemd

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
)

type TimerStatus struct {
	Name                   string `json:"name"`
	Description            string `json:"description"`
	LoadState              string `json:"load_state"`
	ActiveState            string `json:"active_state"`
	SubState               string `json:"sub_state"`
	UnitFileState          string `json:"unit_file_state"`
	ActiveEnterTimestamp   uint64 `json:"active_enter_timestamp"`
	InactiveEnterTimestamp uint64 `json:"inactive_enter_timestamp"`
	NextElapseUSec         uint64 `json:"next_elapse_usec"`
	LastTriggerUSec        uint64 `json:"last_trigger_usec"`
	Unit                   string `json:"unit"`
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
	timer := TimerStatus{
		Name:          entry.Name,
		Description:   entry.Description,
		LoadState:     entry.LoadState,
		ActiveState:   entry.ActiveState,
		SubState:      entry.SubState,
		UnitFileState: entry.UnitFileState,
	}
	if entry.Path == "" {
		return timer
	}

	unit := session.ObjectAt(entry.Path)
	if state, ok := getStringProperty(session, unit, dbusclient.SystemdUnitIface, "UnitFileState"); ok {
		timer.UnitFileState = state
	}
	if ts, ok := getUint64Property(session, unit, dbusclient.SystemdUnitIface, "ActiveEnterTimestamp"); ok {
		timer.ActiveEnterTimestamp = ts
	}
	if ts, ok := getUint64Property(session, unit, dbusclient.SystemdUnitIface, "InactiveEnterTimestamp"); ok {
		timer.InactiveEnterTimestamp = ts
	}
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
