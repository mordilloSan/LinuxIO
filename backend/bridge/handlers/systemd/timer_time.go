package systemd

import (
	"math"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
)

const usecPerSecond = 1_000_000

var (
	timerNow                    = time.Now
	readSystemMonotonicUsecFunc = readSystemMonotonicUsec
)

func timerNextElapseUsec(session dbusclient.SystemSession, unit dbusclient.BusObject) (uint64, bool) {
	if next, ok := getUint64Property(session, unit, dbusclient.SystemdTimerIface, "NextElapseUSecRealtime"); ok && next > 0 {
		return next, true
	}
	if next, ok := getUint64Property(session, unit, dbusclient.SystemdTimerIface, "NextElapseUSecMonotonic"); ok && next > 0 {
		if converted, ok := monotonicTimerUsecToRealtimeUsec(next); ok {
			return converted, true
		}
	}
	return 0, false
}

func timerNextElapseVariantUsec(session dbusclient.SystemSession, unit dbusclient.BusObject) (uint64, bool) {
	if val, err := dbusclient.GetVariantProperty(session.Context(), unit, dbusclient.SystemdTimerIface, "NextElapseUSecRealtime"); err == nil {
		if next, ok := val.Value().(uint64); ok && next > 0 {
			return next, true
		}
	}
	if val, err := dbusclient.GetVariantProperty(session.Context(), unit, dbusclient.SystemdTimerIface, "NextElapseUSecMonotonic"); err == nil {
		if next, ok := val.Value().(uint64); ok && next > 0 {
			if converted, ok := monotonicTimerUsecToRealtimeUsec(next); ok {
				return converted, true
			}
		}
	}
	return 0, false
}

func monotonicTimerUsecToRealtimeUsec(nextMonotonic uint64) (uint64, bool) {
	currentMonotonic, ok := readSystemMonotonicUsecFunc()
	if !ok {
		return 0, false
	}

	now := timerNow()
	if nextMonotonic <= currentMonotonic {
		return uint64(now.UnixMicro()), true
	}

	delta := nextMonotonic - currentMonotonic
	maxDurationUsec := uint64(math.MaxInt64 / int64(time.Microsecond))
	if delta > maxDurationUsec {
		return 0, false
	}
	return uint64(now.Add(time.Duration(delta) * time.Microsecond).UnixMicro()), true
}

func readSystemMonotonicUsec() (uint64, bool) {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return 0, false
	}
	if ts.Sec < 0 || ts.Nsec < 0 {
		return 0, false
	}
	return uint64(ts.Sec)*usecPerSecond + uint64(ts.Nsec)/1_000, true
}
