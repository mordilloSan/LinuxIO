package alerts

import (
	"testing"
	"time"
)

func TestRanFor(t *testing.T) {
	for _, tc := range []struct {
		ran  time.Duration
		want string
	}{
		{0, "It ran for less than 1 s."},
		{999 * time.Millisecond, "It ran for less than 1 s."},
		{time.Second, "It ran for 1 s."},
		{12 * time.Second, "It ran for 12 s."},
		{59*time.Second + 900*time.Millisecond, "It ran for 59 s."},
		{time.Minute, "It ran for 1 min."},
		{5 * time.Minute, "It ran for 5 min."},
		{5*time.Minute - time.Second, "It ran for 4 min."},
		{59*time.Minute + 59*time.Second, "It ran for 59 min."},
		{time.Hour, "It ran for 1 h."},
		{3*time.Hour + 12*time.Minute, "It ran for 3 h 12 min."},
		{3*time.Hour + 12*time.Minute + 59*time.Second, "It ran for 3 h 12 min."},
		{26 * time.Hour, "It ran for 26 h."},
		{-time.Second, "It ran for less than 1 s."}, // a clock set back during the run
	} {
		if got := RanFor(tc.ran); got != tc.want {
			t.Errorf("RanFor(%v) = %q, want %q", tc.ran, got, tc.want)
		}
	}
}
