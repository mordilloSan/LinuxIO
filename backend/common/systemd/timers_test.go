package systemd

import (
	"testing"
	"time"
)

func TestMonotonicTimerIntervalPrefersRecurringCadence(t *testing.T) {
	got, ok := monotonicTimerInterval([][]any{
		{"OnActiveUSec", uint64((5 * time.Minute) / time.Microsecond), uint64(0)},
		{"OnUnitActiveUSec", uint64((30 * time.Minute) / time.Microsecond), uint64(0)},
	})
	if !ok || got != 30*time.Minute {
		t.Fatalf("monotonicTimerInterval() = %s, %v; want 30m", got, ok)
	}
}
