package btrfs

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func useTempRules(t *testing.T) {
	t.Helper()
	old := RulesPath
	RulesPath = filepath.Join(t.TempDir(), "btrfs-snapshots.json")
	t.Cleanup(func() { RulesPath = old })
}

func TestRulesRoundTrip(t *testing.T) {
	useTempRules(t)

	got, err := LoadRules()
	if err != nil || len(got) != 0 {
		t.Fatalf("missing file: got %v, %v; want empty", got, err)
	}

	a := BtrfsSnapshotRule{UUID: "u1", Path: "@home", Enabled: true, Every: "hourly", Keep: BtrfsKeep{Hourly: 24, Daily: 7}}
	b := BtrfsSnapshotRule{UUID: "u1", Path: "@", Every: "daily", BeforeUpdates: true}
	rules := UpsertRule(nil, a)
	rules = UpsertRule(rules, b)
	if _, err = UpdateRules(t.Context(), func(r *[]BtrfsSnapshotRule) error { *r = rules; return nil }); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(RulesPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("stat = %v, %v; want mode 0600", info, err)
	}
	got, err = LoadRules()
	if err != nil || !slices.Equal(got, rules) {
		t.Fatalf("LoadRules = %v, %v; want %v", got, err, rules)
	}

	// Identity is (uuid, path): same key replaces, others append.
	a.Every = "weekly"
	rules = UpsertRule(rules, a)
	if len(rules) != 2 || rules[0].Every != "weekly" {
		t.Fatalf("upsert replace = %v", rules)
	}
	rules = DeleteRule(rules, "u1", "@home")
	if len(rules) != 1 || rules[0].Path != "@" {
		t.Fatalf("delete = %v", rules)
	}
}

func TestValidateRuleRejectsBadEvery(t *testing.T) {
	ok := BtrfsSnapshotRule{UUID: "u1", Path: "@home", Every: "daily"}
	if err := ValidateRule(ok); err != nil {
		t.Fatalf("valid rule rejected: %v", err)
	}
	bad := map[string]BtrfsSnapshotRule{
		"every":    {UUID: "u1", Path: "@home", Every: "monthly"},
		"uuid":     {Path: "@home", Every: "daily"},
		"path":     {UUID: "u1", Path: "/abs", Every: "daily"},
		"reserved": {UUID: "u1", Path: SnapshotRoot + "/x", Every: "daily"},
		"keep":     {UUID: "u1", Path: "@home", Every: "daily", Keep: BtrfsKeep{Weekly: -1}},
		"keepNone": {UUID: "u1", Path: "@home", Enabled: true, Every: "daily"},
		"hour":     {UUID: "u1", Path: "@home", Every: "daily", Hour: 24},
		"weekday":  {UUID: "u1", Path: "@home", Every: "weekly", Weekday: 7},
	}
	for name, r := range bad {
		if err := ValidateRule(r); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestDueHourlyWithTolerance(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	rule := BtrfsSnapshotRule{Every: "hourly"}
	at := func(ago time.Duration) *time.Time { v := now.Add(-ago); return &v }

	if !Due(rule, nil, now) {
		t.Error("nil newest should be due")
	}
	if !Due(rule, at(59*time.Minute), now) {
		t.Error("59m should be due (tolerance)")
	}
	if Due(rule, at(50*time.Minute), now) {
		t.Error("50m should not be due")
	}
	// Daily rules follow their hour slot (00:00 here), not a 24h period.
	if Due(BtrfsSnapshotRule{Every: "daily"}, at(11*time.Hour), now) {
		t.Error("daily: snapshot after today's 00:00 should not be due")
	}
	if !Due(BtrfsSnapshotRule{Every: "daily"}, at(13*time.Hour), now) {
		t.Error("daily: snapshot before today's 00:00 should be due")
	}
	if !Due(BtrfsSnapshotRule{Every: "weekly"}, at(7*24*time.Hour), now) {
		t.Error("weekly at 7d should be due")
	}
}

func TestDueAtScheduledTime(t *testing.T) {
	// Wednesday 2026-10-21 03:02 local.
	now := time.Date(2026, 10, 21, 3, 2, 0, 0, time.Local)
	daily := BtrfsSnapshotRule{Every: "daily", Hour: 3}
	before := now.Add(-10 * time.Minute) // 02:52, before today's 03:00
	after := now.Add(-time.Minute)       // 03:01, after it
	if !Due(daily, &before, now) {
		t.Error("daily: snapshot from before 03:00 should be due at 03:02")
	}
	if Due(daily, &after, now) {
		t.Error("daily: snapshot taken after 03:00 should not be due again")
	}
	if Due(daily, &before, now.Add(-8*time.Minute)) {
		t.Error("daily: 02:54 is before the 03:00 slot minus tolerance")
	}
	if !Due(daily, &before, now.Add(-7*time.Minute)) {
		t.Error("daily: 02:55 is within the tolerance of 03:00")
	}

	weekly := BtrfsSnapshotRule{Every: "weekly", Hour: 3, Weekday: int(time.Monday)}
	lastSunday := time.Date(2026, 10, 18, 12, 0, 0, 0, time.Local)
	thisMonday := time.Date(2026, 10, 19, 3, 30, 0, 0, time.Local)
	if !Due(weekly, &lastSunday, now) {
		t.Error("weekly: nothing since Monday 03:00 should be due")
	}
	if Due(weekly, &thisMonday, now) {
		t.Error("weekly: a snapshot after Monday 03:00 should not be due")
	}
	if got := lastScheduled(weekly, now); got.Weekday() != time.Monday || got.Hour() != 3 || got.Day() != 19 {
		t.Errorf("lastScheduled = %v, want Monday 19th 03:00", got)
	}
}
