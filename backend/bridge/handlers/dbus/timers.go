package dbus

import (
	"sync"

	godbus "github.com/godbus/dbus/v5"
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

func ListTimers() ([]TimerStatus, error) {
	var timers []TimerStatus
	err := withSystemdManager(func(conn *godbus.Conn, systemd godbus.BusObject) error {
		entries, err := listUnitsBySuffix(systemd, ".timer")
		if err != nil {
			return err
		}

		results := make([]TimerStatus, len(entries))
		var wg sync.WaitGroup
		for i, entry := range entries {
			wg.Add(1)
			go func(i int, entry listedUnit) {
				defer wg.Done()
				timer := TimerStatus{
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
						timer.UnitFileState = state
					}
					if ts, ok := getUint64Property(unit, "org.freedesktop.systemd1.Unit.ActiveEnterTimestamp"); ok {
						timer.ActiveEnterTimestamp = ts
					}
					if ts, ok := getUint64Property(unit, "org.freedesktop.systemd1.Unit.InactiveEnterTimestamp"); ok {
						timer.InactiveEnterTimestamp = ts
					}
					if next, ok := getUint64Property(unit, "org.freedesktop.systemd1.Timer.NextElapseUSecRealtime"); ok && next > 0 {
						timer.NextElapseUSec = next
					}
					if timer.NextElapseUSec == 0 {
						if next, ok := getUint64Property(unit, "org.freedesktop.systemd1.Timer.NextElapseUSecMonotonic"); ok {
							timer.NextElapseUSec = next
						}
					}
					if last, ok := getUint64Property(unit, "org.freedesktop.systemd1.Timer.LastTriggerUSec"); ok {
						timer.LastTriggerUSec = last
					}
					if target, ok := getStringProperty(unit, "org.freedesktop.systemd1.Timer.Unit"); ok {
						timer.Unit = target
					}
				}

				results[i] = timer
			}(i, entry)
		}

		wg.Wait()
		timers = results
		return nil
	})
	return timers, err
}
