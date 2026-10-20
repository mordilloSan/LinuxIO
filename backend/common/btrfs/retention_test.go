package btrfs

import (
	"testing"
	"time"
)

func snapAt(kind SnapshotKind, at time.Time) BtrfsSnapshot {
	return BtrfsSnapshot{Source: "@home", Path: SnapshotPath("@home", at, kind), Kind: kind, CreatedAt: at}
}

func TestPruneKeepsBuckets(t *testing.T) {
	// 30 hourly snapshots, newest 2026-10-19T12:00Z, oldest 2026-10-18T07:00Z:
	// hours 07..23 on the 18th (17) and 00..12 on the 19th (13).
	newest := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	var snaps []BtrfsSnapshot
	for i := range 30 {
		snaps = append(snaps, snapAt(KindTimeline, newest.Add(-time.Duration(i)*time.Hour)))
	}

	del := Prune(snaps, BtrfsKeep{Hourly: 24, Daily: 7})

	// Hourly keeps the 24 newest (19th 12:00 back to 18th 13:00). Daily keeps
	// the newest of the 19th (already kept) and of the 18th (18th 23:00, already
	// kept). So the survivors are exactly those 24; the oldest 6 go.
	if len(del) != 6 {
		t.Fatalf("deleted %d, want 6: %v", len(del), del)
	}
	for i, s := range del {
		want := newest.Add(-time.Duration(29-i) * time.Hour)
		if !s.CreatedAt.Equal(want) {
			t.Errorf("del[%d] = %v, want %v (oldest first)", i, s.CreatedAt, want)
		}
	}

	// Smaller hourly window: daily bucket rescues the newest of the older day.
	del = Prune(snaps, BtrfsKeep{Hourly: 12, Daily: 7})
	deleted := map[time.Time]bool{}
	for _, s := range del {
		deleted[s.CreatedAt] = true
	}
	day18 := time.Date(2026, 10, 18, 23, 0, 0, 0, time.UTC)
	if deleted[day18] {
		t.Error("newest snapshot of 2026-10-18 must survive via the daily bucket")
	}
	if len(del) != 30-12-1 {
		t.Errorf("deleted %d, want %d", len(del), 30-12-1)
	}
}

func TestPruneWeekMonthBuckets(t *testing.T) {
	snaps := []BtrfsSnapshot{
		snapAt(KindTimeline, time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)),
		snapAt(KindTimeline, time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)), // previous ISO week
		snapAt(KindTimeline, time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)), // same week, older
		snapAt(KindTimeline, time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)),
		snapAt(KindTimeline, time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)),
	}
	del := Prune(snaps, BtrfsKeep{Weekly: 2, Monthly: 2})
	// weekly keeps 10-19 and 10-14; monthly keeps 10-19 and 09-30.
	if len(del) != 2 || !del[0].CreatedAt.Equal(snaps[4].CreatedAt) || !del[1].CreatedAt.Equal(snaps[2].CreatedAt) {
		t.Fatalf("del = %v", del)
	}
}

func TestPruneNeverDeletesManual(t *testing.T) {
	base := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	var snaps []BtrfsSnapshot
	for i := range 5 {
		at := base.Add(-time.Duration(i) * 24 * time.Hour)
		snaps = append(snaps, snapAt(KindManual, at), snapAt(KindPreUpdate, at), snapAt(KindPreRollback, at))
	}
	del := Prune(snaps, BtrfsKeep{})
	counts := map[SnapshotKind]int{}
	for _, s := range del {
		counts[s.Kind]++
		if !s.CreatedAt.Before(base.Add(-2 * 24 * time.Hour)) {
			t.Errorf("deleted a newest-3 snapshot: %v", s.Path)
		}
	}
	if counts[KindManual] != 0 {
		t.Errorf("manual deleted: %d", counts[KindManual])
	}
	if counts[KindPreUpdate] != 2 || counts[KindPreRollback] != 2 {
		t.Errorf("pre-update/pre-rollback deleted = %d/%d, want 2/2", counts[KindPreUpdate], counts[KindPreRollback])
	}
}
