package btrfs

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"
)

// keepNewestPerKind is how many pre-update and pre-rollback snapshots survive.
const keepNewestPerKind = 3

// keepBucket is one retention bucket: how many distinct buckets to keep and
// the key that groups snapshot times into them.
type keepBucket struct {
	n   int
	key func(time.Time) string
}

// buckets lists every BtrfsKeep count with its bucket key. ValidateRule and
// Prune both read the counts from here, so a field missing from this list
// fails TestKeepBucketsCoverEveryField.
func (k BtrfsKeep) buckets() []keepBucket {
	return []keepBucket{
		{k.Hourly, func(t time.Time) string { return t.Truncate(time.Hour).Format(time.RFC3339) }},
		{k.Daily, func(t time.Time) string { return t.Format(time.DateOnly) }},
		{k.Weekly, func(t time.Time) string { y, w := t.ISOWeek(); return fmt.Sprintf("%d-W%02d", y, w) }},
		{k.Monthly, func(t time.Time) string { return t.Format("2006-01") }},
	}
}

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
	for _, b := range keep.buckets() {
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

// KeptReason says why a snapshot is still there: what keeps it, or that the
// next snapshot pass deletes it.
type KeptReason string

const (
	KeptManual        KeptReason = "manual"        // taken by hand, never pruned
	KeptUnscheduled   KeptReason = "unscheduled"   // no rule covers its source
	KeptScheduleOff   KeptReason = "scheduleOff"   // its rule is off, which leaves this kind alone
	KeptRecent        KeptReason = "recent"        // one of the newest keepNewestPerKind of its kind
	KeptHourly        KeptReason = "hourly"        // the newest of one of the hours the rule keeps
	KeptDaily         KeptReason = "daily"         // ... of the days
	KeptWeekly        KeptReason = "weekly"        // ... of the ISO weeks
	KeptMonthly       KeptReason = "monthly"       // ... of the months
	KeptReplication   KeptReason = "replication"   // the parent the next incremental send needs
	KeptUntilNextPass KeptReason = "untilNextPass" // the next pass deletes it
)

// bucketReasons names keep.buckets() in their order.
var bucketReasons = []KeptReason{KeptHourly, KeptDaily, KeptWeekly, KeptMonthly}

// KeptBy says, by path, why each snapshot of one source survives, as the
// snapshot pass would decide it (runRule, Prune, protectReplicationParent):
// rule is the source's rule, nil when none, and protectsParent says an
// enabled timeline replication job covers it. Prune is not used: it is the
// deleting side and stays as it is; TestKeptByAgreesWithPrune holds the two
// to the same answer.
func KeptBy(snaps []BtrfsSnapshot, rule *BtrfsSnapshotRule, protectsParent bool) map[string]KeptReason {
	reasons := make(map[string]KeptReason, len(snaps))
	newestFirst := slices.Clone(snaps)
	slices.SortStableFunc(newestFirst, func(a, b BtrfsSnapshot) int { return b.CreatedAt.Compare(a.CreatedAt) })

	var timeline []BtrfsSnapshot
	seenOfKind := map[SnapshotKind]int{}
	for _, s := range newestFirst {
		if s.Kind == KindTimeline && rule != nil && rule.Enabled {
			timeline = append(timeline, s)
			continue
		}
		reasons[s.Path] = keptOutsideTimeline(s, rule, seenOfKind)
	}
	maps.Copy(reasons, keptTimeline(timeline, rule.keepBuckets(), protectsParent))
	return reasons
}

// keptOutsideTimeline is the reason for a snapshot the rule's buckets do not
// decide; seenOfKind counts the newer ones of each kind already given one.
func keptOutsideTimeline(s BtrfsSnapshot, rule *BtrfsSnapshotRule, seenOfKind map[SnapshotKind]int) KeptReason {
	switch {
	case s.Kind == KindManual:
		return KeptManual
	case rule == nil:
		return KeptUnscheduled
	case s.Kind == KindPreUpdate && (rule.Enabled || rule.BeforeUpdates),
		s.Kind == KindPreRollback && rule.Enabled:
		if seenOfKind[s.Kind]++; seenOfKind[s.Kind] <= keepNewestPerKind {
			return KeptRecent
		}
		return KeptUntilNextPass
	default:
		return KeptScheduleOff
	}
}

// keptTimeline gives each timeline snapshot, newest first, the smallest
// bucket that keeps it, as Prune counts them; one no bucket keeps is the
// replication parent when protected and among the two newest, else removed.
func keptTimeline(timeline []BtrfsSnapshot, buckets []keepBucket, protectsParent bool) map[string]KeptReason {
	reasons := make(map[string]KeptReason, len(timeline))
	for bi, b := range buckets {
		seen := map[string]bool{}
		for _, s := range timeline {
			if len(seen) >= b.n {
				break
			}
			if k := b.key(s.CreatedAt.UTC()); !seen[k] {
				seen[k] = true
				if _, ok := reasons[s.Path]; !ok {
					reasons[s.Path] = bucketReasons[bi]
				}
			}
		}
	}
	for i, s := range timeline {
		if _, ok := reasons[s.Path]; ok {
			continue
		}
		if protectsParent && i < 2 {
			reasons[s.Path] = KeptReplication
		} else {
			reasons[s.Path] = KeptUntilNextPass
		}
	}
	return reasons
}

// keepBuckets is the rule's buckets, none for no rule.
func (r *BtrfsSnapshotRule) keepBuckets() []keepBucket {
	if r == nil {
		return nil
	}
	return r.Keep.buckets()
}

// ExplainKept sets KeptBy on snapshots of the filesystem uuid from the
// snapshot rules and replication jobs. When either cannot be read, as by a
// session without sudo, the reasons stay empty.
func ExplainKept(snaps []BtrfsSnapshot, uuid string) {
	rules, err := LoadRules()
	if err != nil {
		slog.Debug("snapshot retention reasons unavailable", "uuid", uuid, "error", err)
		return
	}
	jobs, err := LoadJobs()
	if err != nil {
		slog.Debug("snapshot retention reasons unavailable", "uuid", uuid, "error", err)
		return
	}
	bySource := map[string][]BtrfsSnapshot{}
	for _, s := range snaps {
		bySource[s.Source] = append(bySource[s.Source], s)
	}
	reasons := map[string]KeptReason{}
	for source, group := range bySource {
		var rule *BtrfsSnapshotRule
		if i := slices.IndexFunc(rules, func(r BtrfsSnapshotRule) bool { return r.UUID == uuid && r.Path == source }); i >= 0 {
			rule = &rules[i]
		}
		protects := slices.ContainsFunc(jobs, func(j BtrfsReplicationJob) bool {
			return j.Enabled && j.After == AfterTimeline && j.UUID == uuid && j.Path == source
		})
		maps.Copy(reasons, KeptBy(group, rule, protects))
	}
	for i := range snaps {
		snaps[i].KeptBy = reasons[snaps[i].Path]
	}
}
