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
	if Due(BtrfsSnapshotRule{Every: "daily"}, at(23*time.Hour), now) {
		t.Error("daily at 23h should not be due")
	}
	if !Due(BtrfsSnapshotRule{Every: "weekly"}, at(7*24*time.Hour), now) {
		t.Error("weekly at 7d should be due")
	}
}
