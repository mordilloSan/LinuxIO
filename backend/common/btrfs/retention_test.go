package btrfs

import (
	"encoding/json/v2"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"
)

// TestKeepBucketsCoverEveryField fails when an int field is added to
// BtrfsKeep without its entry in BtrfsKeep.buckets, the one list that
// ValidateRule and Prune read the counts from.
func TestKeepBucketsCoverEveryField(t *testing.T) {
	typ := reflect.TypeFor[BtrfsKeep]()
	var fields []int
	for i := range typ.NumField() {
		if typ.Field(i).Type.Kind() == reflect.Int {
			fields = append(fields, i)
		}
	}
	if got := len(BtrfsKeep{}.buckets()); got != len(fields) {
		t.Fatalf("buckets() has %d entries, BtrfsKeep has %d int fields", got, len(fields))
	}

	snap := snapAt(KindTimeline, time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC))
	for _, i := range fields {
		name := typ.Field(i).Name
		only := func(n int) BtrfsKeep {
			var keep BtrfsKeep
			reflect.ValueOf(&keep).Elem().Field(i).SetInt(int64(n))
			return keep
		}
		rule := BtrfsSnapshotRule{UUID: "u1", Path: "@home", Enabled: true, Every: EveryDaily}

		rule.Keep = only(1)
		if err := ValidateRule(rule); err != nil {
			t.Errorf("only %s = 1: rejected: %v", name, err)
		}
		if del := Prune([]BtrfsSnapshot{snap}, rule.Keep); len(del) != 0 {
			t.Errorf("only %s = 1: Prune deleted %v, want the snapshot kept", name, del)
		}

		rule.Keep = only(-1)
		if err := ValidateRule(rule); err == nil || err.Error() != "keep counts must not be negative" {
			t.Errorf("only %s = -1: got %v, want the negative-count error", name, err)
		}
	}
}

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

func TestKeptBy(t *testing.T) {
	newest := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	thisHour := snapAt(KindTimeline, newest)
	earlierToday := snapAt(KindTimeline, newest.Add(-time.Hour))
	yesterday := snapAt(KindTimeline, newest.Add(-24*time.Hour))
	byHand := snapAt(KindManual, newest.Add(-48*time.Hour))
	var updates []BtrfsSnapshot
	for i := range 4 {
		updates = append(updates, snapAt(KindPreUpdate, newest.Add(-time.Duration(i+1)*time.Minute)))
	}
	all := append([]BtrfsSnapshot{thisHour, earlierToday, yesterday, byHand}, updates...)
	enabled := &BtrfsSnapshotRule{UUID: "u1", Path: "@home", Enabled: true, Every: EveryHourly, Keep: BtrfsKeep{Hourly: 1, Daily: 2}}

	for _, tc := range []struct {
		name    string
		rule    *BtrfsSnapshotRule
		protect bool
		want    map[string]KeptReason
	}{
		{
			name: "an enabled rule names the smallest bucket that keeps each",
			rule: enabled,
			want: map[string]KeptReason{
				thisHour.Path: KeptHourly, earlierToday.Path: KeptUntilNextPass, yesterday.Path: KeptDaily,
				byHand.Path:     KeptManual,
				updates[0].Path: KeptRecent, updates[1].Path: KeptRecent, updates[2].Path: KeptRecent,
				updates[3].Path: KeptUntilNextPass,
			},
		},
		{
			name: "the replication parent is kept",
			rule: enabled, protect: true,
			want: map[string]KeptReason{
				thisHour.Path: KeptHourly, earlierToday.Path: KeptReplication, yesterday.Path: KeptDaily,
				byHand.Path:     KeptManual,
				updates[0].Path: KeptRecent, updates[1].Path: KeptRecent, updates[2].Path: KeptRecent,
				updates[3].Path: KeptUntilNextPass,
			},
		},
		{
			name: "a disabled rule leaves its timeline but prunes before-update snapshots",
			rule: &BtrfsSnapshotRule{UUID: "u1", Path: "@home", Every: EveryHourly, Keep: BtrfsKeep{Hourly: 1}, BeforeUpdates: true},
			want: map[string]KeptReason{
				thisHour.Path: KeptScheduleOff, earlierToday.Path: KeptScheduleOff, yesterday.Path: KeptScheduleOff,
				byHand.Path:     KeptManual,
				updates[0].Path: KeptRecent, updates[1].Path: KeptRecent, updates[2].Path: KeptRecent,
				updates[3].Path: KeptUntilNextPass,
			},
		},
		{
			name: "a disabled rule without before-update snapshots prunes nothing",
			rule: &BtrfsSnapshotRule{UUID: "u1", Path: "@home", Every: EveryHourly, Keep: BtrfsKeep{Hourly: 1}},
			want: map[string]KeptReason{
				thisHour.Path: KeptScheduleOff, earlierToday.Path: KeptScheduleOff, yesterday.Path: KeptScheduleOff,
				byHand.Path:     KeptManual,
				updates[0].Path: KeptScheduleOff, updates[1].Path: KeptScheduleOff, updates[2].Path: KeptScheduleOff,
				updates[3].Path: KeptScheduleOff,
			},
		},
		{
			name: "without a rule nothing prunes",
			want: map[string]KeptReason{
				thisHour.Path: KeptUnscheduled, earlierToday.Path: KeptUnscheduled, yesterday.Path: KeptUnscheduled,
				byHand.Path:     KeptManual,
				updates[0].Path: KeptUnscheduled, updates[1].Path: KeptUnscheduled, updates[2].Path: KeptUnscheduled,
				updates[3].Path: KeptUnscheduled,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := KeptBy(all, tc.rule, tc.protect); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("KeptBy =\n%v\nwant\n%v", got, tc.want)
			}
		})
	}
}

func TestKeptByRollbacks(t *testing.T) {
	newest := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	var rollbacks []BtrfsSnapshot
	for i := range 4 {
		rollbacks = append(rollbacks, snapAt(KindPreRollback, newest.Add(-time.Duration(i)*time.Minute)))
	}
	enabled := &BtrfsSnapshotRule{UUID: "u1", Path: "@home", Enabled: true, Every: EveryHourly, Keep: BtrfsKeep{Hourly: 1}}
	got := KeptBy(rollbacks, enabled, false)
	want := []KeptReason{KeptRecent, KeptRecent, KeptRecent, KeptUntilNextPass}
	for i, s := range rollbacks {
		if got[s.Path] != want[i] {
			t.Fatalf("enabled rule, rollback %d kept as %q, want %q", i, got[s.Path], want[i])
		}
	}
	// A disabled rule runs only for updates, which leaves rollbacks alone.
	disabled := &BtrfsSnapshotRule{UUID: "u1", Path: "@home", Every: EveryHourly, BeforeUpdates: true}
	for path, reason := range KeptBy(rollbacks, disabled, false) {
		if reason != KeptScheduleOff {
			t.Fatalf("disabled rule, %s kept as %q", path, reason)
		}
	}
}

// KeptBy explains Prune: with an enabled rule and no replication, a
// snapshot is kept until the next pass exactly when Prune deletes it.
func TestKeptByAgreesWithPrune(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	kinds := []SnapshotKind{KindTimeline, KindTimeline, KindTimeline, KindPreUpdate, KindPreRollback, KindManual}
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	for round := range 200 {
		var snaps []BtrfsSnapshot
		for range r.IntN(60) {
			at := start.Add(time.Duration(r.IntN(90*24*60)) * time.Minute)
			snaps = append(snaps, snapAt(kinds[r.IntN(len(kinds))], at))
		}
		rule := &BtrfsSnapshotRule{UUID: "u1", Path: "@home", Enabled: true, Every: EveryHourly,
			Keep: BtrfsKeep{Hourly: r.IntN(6), Daily: r.IntN(6), Weekly: r.IntN(4), Monthly: r.IntN(3)}}

		deleted := map[string]bool{}
		for _, s := range Prune(snaps, rule.Keep) {
			deleted[s.Path] = true
		}
		reasons := KeptBy(snaps, rule, false)
		paths := map[string]bool{}
		for _, s := range snaps {
			paths[s.Path] = true
		}
		if len(reasons) != len(paths) {
			t.Fatalf("round %d: %d reasons for %d snapshots", round, len(reasons), len(paths))
		}
		for path, reason := range reasons {
			if (reason == KeptUntilNextPass) != deleted[path] {
				t.Fatalf("round %d: %s kept as %q, Prune deletes it: %v", round, path, reason, deleted[path])
			}
		}
	}
}

func TestExplainKeptReadsRulesAndJobs(t *testing.T) {
	dir := t.TempDir()
	oldRules, oldJobs := RulesPath, ReplicationPath
	RulesPath, ReplicationPath = filepath.Join(dir, "rules.json"), filepath.Join(dir, "jobs.json")
	t.Cleanup(func() { RulesPath, ReplicationPath = oldRules, oldJobs })
	rule := BtrfsSnapshotRule{UUID: "u1", Path: "@home", Enabled: true, Every: EveryHourly, Keep: BtrfsKeep{Hourly: 1}}
	data, err := json.Marshal(rulesDocument{Rules: []BtrfsSnapshotRule{rule}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(RulesPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// Only an enabled timeline job of this pool and subvolume protects the
	// replication parent.
	jobs, err := json.Marshal(jobsDocument{Jobs: []BtrfsReplicationJob{
		{ID: "a", UUID: "u1", Path: "@home", Enabled: true, After: AfterTimeline},
		{ID: "b", UUID: "u2", Path: "@var", Enabled: true, After: AfterTimeline},
		{ID: "c", UUID: "u1", Path: "@var", After: AfterTimeline},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(ReplicationPath, jobs, 0o600); err != nil {
		t.Fatal(err)
	}

	newest := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	home := snapAt(KindTimeline, newest)
	homeEarlier := snapAt(KindTimeline, newest.Add(-10*time.Minute))
	other := snapAt(KindTimeline, newest)
	other.Source, other.Path = "@var", SnapshotPath("@var", newest, KindTimeline)
	snaps := []BtrfsSnapshot{home, homeEarlier, other}

	ExplainKept(snaps, "u1")

	want := []KeptReason{KeptHourly, KeptReplication, KeptUnscheduled}
	if got := []KeptReason{snaps[0].KeptBy, snaps[1].KeptBy, snaps[2].KeptBy}; !slices.Equal(got, want) {
		t.Fatalf("kept by = %v, want %v", got, want)
	}

	// A rule file that cannot be read leaves the reasons empty.
	if err = os.WriteFile(RulesPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	snaps = []BtrfsSnapshot{home}
	ExplainKept(snaps, "u1")
	if snaps[0].KeptBy != "" {
		t.Fatalf("kept by = %q with an unreadable rule file", snaps[0].KeptBy)
	}
}
