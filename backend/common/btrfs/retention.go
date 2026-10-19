package btrfs

import (
	"fmt"
	"slices"
	"time"
)

// keepNewestPerKind is how many pre-update and pre-rollback snapshots survive.
const keepNewestPerKind = 3

// Prune returns the snapshots to delete, oldest first. Timeline snapshots are
// kept per hour/day/week/month bucket (the newest of each of the most recent
// N distinct buckets) and survive if any bucket keeps them. Pre-update and
// pre-rollback snapshots keep the newest three of each kind. Manual snapshots
// are never returned.
func Prune(snaps []BtrfsSnapshot, keep BtrfsKeep) []BtrfsSnapshot {
	newestFirst := slices.Clone(snaps)
	slices.SortStableFunc(newestFirst, func(a, b BtrfsSnapshot) int { return b.CreatedAt.Compare(a.CreatedAt) })

	var timeline []BtrfsSnapshot
	kept := map[SnapshotKind]int{}
	var del []BtrfsSnapshot
	for _, s := range newestFirst {
		switch s.Kind {
		case KindManual:
			// never pruned
		case KindTimeline:
			timeline = append(timeline, s)
		case KindPreUpdate, KindPreRollback:
			if kept[s.Kind]++; kept[s.Kind] > keepNewestPerKind {
				del = append(del, s)
			}
		}
	}

	survives := make([]bool, len(timeline))
	for _, b := range []struct {
		n   int
		key func(time.Time) string
	}{
		{keep.Hourly, func(t time.Time) string { return t.Truncate(time.Hour).Format(time.RFC3339) }},
		{keep.Daily, func(t time.Time) string { return t.Format(time.DateOnly) }},
		{keep.Weekly, func(t time.Time) string { y, w := t.ISOWeek(); return fmt.Sprintf("%d-W%02d", y, w) }},
		{keep.Monthly, func(t time.Time) string { return t.Format("2006-01") }},
	} {
		seen := map[string]bool{}
		for i, s := range timeline {
			if len(seen) >= b.n {
				break
			}
			if k := b.key(s.CreatedAt.UTC()); !seen[k] {
				seen[k] = true
				survives[i] = true
			}
		}
	}
	for i, s := range timeline {
		if !survives[i] {
			del = append(del, s)
		}
	}

	slices.SortStableFunc(del, func(a, b BtrfsSnapshot) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return del
}
