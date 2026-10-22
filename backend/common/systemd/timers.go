package systemd

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
)

// GetTimerInterval returns the systemd-owned monotonic cadence. A disabled or
// masked timer has a zero interval.
func GetTimerInterval(ctx context.Context, name string) (time.Duration, error) {
	if err := requireUnitName(name); err != nil {
		return 0, err
	}

	var interval time.Duration
	err := dbusclient.SystemdManager.UseSession(ctx, func(session dbusclient.SystemSession) error {
		path, err := UnitObjectPath(session, name)
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
