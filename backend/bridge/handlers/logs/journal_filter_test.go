package logs

import (
	"testing"
	"time"
)

func TestJournalPeriodCutoff(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, tc := range []struct {
		period string
		want   time.Duration
	}{
		{period: "1h", want: time.Hour},
		{period: "24h", want: 24 * time.Hour},
		{period: "7d", want: 7 * 24 * time.Hour},
		{period: "30d", want: 30 * 24 * time.Hour},
	} {
		cutoff, ok := journalPeriodCutoff(tc.period, now)
		if !ok {
			t.Fatalf("journalPeriodCutoff(%q) was not parsed", tc.period)
		}
		if want := now.Add(-tc.want).UnixMicro(); cutoff != want {
			t.Errorf("journalPeriodCutoff(%q) = %d, want %d", tc.period, cutoff, want)
		}
	}
}

func TestJournalLineOlderThan(t *testing.T) {
	const cutoff = int64(1_700_000_000_000_000)
	if !journalLineOlderThan(
		`{"__REALTIME_TIMESTAMP":"1699999999999999","MESSAGE":"old"}`,
		cutoff,
	) {
		t.Error("entry before cutoff was not recognized")
	}
	if journalLineOlderThan(
		`{"__REALTIME_TIMESTAMP":"1700000000000000","MESSAGE":"boundary"}`,
		cutoff,
	) {
		t.Error("entry exactly at cutoff should remain in the window")
	}
	if journalLineOlderThan(`{"MESSAGE":"missing timestamp"}`, cutoff) {
		t.Error("entry without timestamp should not terminate pagination")
	}
}
