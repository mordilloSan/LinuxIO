package systemd

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
)

// GetTimerInterval returns the systemd-owned monotonic cadence. A disabled or
// masked timer has a zero interval.
func GetTimerInterval(ctx context.Context, name string) (time.Duration, error) {
	if err := requireUnitName(name); err != nil {
		return 0, err
	}

	var interval time.Duration
	err := dbusclient.SystemdManager.UseSession(ctx, func(session dbusclient.SystemSession) error {
		path, err := getUnitObjectPath(session, name)
		if err != nil {
			return err
		}
		unit := session.ObjectAt(path)
		state, err := dbusclient.GetProperty[string](session, unit, dbusclient.SystemdUnitIface, "UnitFileState")
		if err != nil {
			return err
		}
		if state == "disabled" || strings.HasPrefix(state, "masked") {
			return nil
		}

		timers, err := dbusclient.GetProperty[[][]any](session, unit, dbusclient.SystemdTimerIface, "TimersMonotonic")
		if err != nil {
			return err
		}
		var ok bool
		interval, ok = monotonicTimerInterval(timers)
		if !ok {
			return fmt.Errorf("timer %s has no supported monotonic interval", name)
		}
		return nil
	})
	return interval, err
}

func monotonicTimerInterval(timers [][]any) (time.Duration, bool) {
	var fallback uint64
	for _, timer := range timers {
		if len(timer) < 2 {
			continue
		}
		base, baseOK := timer[0].(string)
		usec, usecOK := timer[1].(uint64)
		if !baseOK || !usecOK || usec > uint64(math.MaxInt64/int64(time.Microsecond)) {
			continue
		}
		switch base {
		case "OnUnitActiveUSec":
			return time.Duration(usec) * time.Microsecond, true
		case "OnActiveUSec":
			fallback = usec
		}
	}
	if fallback == 0 {
		return 0, false
	}
	return time.Duration(fallback) * time.Microsecond, true
}

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
