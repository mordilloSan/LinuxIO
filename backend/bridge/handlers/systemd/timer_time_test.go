package systemd

import (
	"testing"
	"time"
)

func TestMonotonicTimerUsecToRealtimeUsecConvertsFutureTimer(t *testing.T) {
	withTimerClock(t, time.Unix(1_700_000_000, 0), 10*usecPerSecond)

	got, ok := monotonicTimerUsecToRealtimeUsec(15 * usecPerSecond)
	if !ok {
		t.Fatal("conversion failed")
	}

	want := uint64(time.Unix(1_700_000_005, 0).UnixMicro())
	if got != want {
		t.Fatalf("converted usec = %d, want %d", got, want)
	}
}

func TestMonotonicTimerUsecToRealtimeUsecClampsElapsedTimerToNow(t *testing.T) {
	now := time.Unix(1_700_000_000, 123_000_000)
	withTimerClock(t, now, 10*usecPerSecond)

	got, ok := monotonicTimerUsecToRealtimeUsec(9 * usecPerSecond)
	if !ok {
		t.Fatal("conversion failed")
	}

	want := uint64(now.UnixMicro())
	if got != want {
		t.Fatalf("converted usec = %d, want %d", got, want)
	}
}

func TestMonotonicTimerUsecToRealtimeUsecReturnsFalseWithoutUptime(t *testing.T) {
	origNow := timerNow
	origRead := readSystemMonotonicUsecFunc
	timerNow = func() time.Time { return time.Unix(1_700_000_000, 0) }
	readSystemMonotonicUsecFunc = func() (uint64, bool) { return 0, false }
	t.Cleanup(func() {
		timerNow = origNow
		readSystemMonotonicUsecFunc = origRead
	})

	if got, ok := monotonicTimerUsecToRealtimeUsec(15 * usecPerSecond); ok {
		t.Fatalf("conversion = %d, want unavailable", got)
	}
}

func withTimerClock(t *testing.T, now time.Time, monotonicUsec uint64) {
	t.Helper()
	origNow := timerNow
	origRead := readSystemMonotonicUsecFunc
	timerNow = func() time.Time { return now }
	readSystemMonotonicUsecFunc = func() (uint64, bool) { return monotonicUsec, true }
	t.Cleanup(func() {
		timerNow = origNow
		readSystemMonotonicUsecFunc = origRead
	})
}
