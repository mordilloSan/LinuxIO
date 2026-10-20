package packages

import (
	"context"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

func stubSnapshots(t *testing.T, rules []btrfs.BtrfsSnapshotRule, run func(kind btrfs.SnapshotKind)) {
	t.Helper()
	oldLoad, oldRun := loadSnapshotRules, runSnapshots
	t.Cleanup(func() { loadSnapshotRules, runSnapshots = oldLoad, oldRun })
	loadSnapshotRules = func() ([]btrfs.BtrfsSnapshotRule, error) { return rules, nil }
	runSnapshots = func(_ context.Context, _ []btrfs.BtrfsSnapshotRule, _ time.Time, kind btrfs.SnapshotKind) (btrfs.RunReport, error) {
		run(kind)
		return btrfs.RunReport{}, nil
	}
}

func TestPreUpdateSnapshotSkipsWhenNoRuleOptsIn(t *testing.T) {
	called := false
	stubSnapshots(t, []btrfs.BtrfsSnapshotRule{{UUID: "a", Enabled: true}}, func(btrfs.SnapshotKind) { called = true })
	if _, err := preUpdateSnapshot(t.Context()); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("runSnapshots called without a BeforeUpdates rule")
	}
}

func TestPreUpdateSnapshotRunsForDisabledBeforeUpdatesRule(t *testing.T) {
	called := false
	stubSnapshots(t, []btrfs.BtrfsSnapshotRule{{UUID: "b", BeforeUpdates: true}}, func(btrfs.SnapshotKind) { called = true })
	if _, err := preUpdateSnapshot(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("runSnapshots not called for a disabled BeforeUpdates rule")
	}
}

func TestPreUpdateSnapshotRunsPreUpdateKind(t *testing.T) {
	var got btrfs.SnapshotKind
	stubSnapshots(t, []btrfs.BtrfsSnapshotRule{{UUID: "a", Enabled: true, BeforeUpdates: true}}, func(kind btrfs.SnapshotKind) { got = kind })
	if _, err := preUpdateSnapshot(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got != btrfs.KindPreUpdate {
		t.Fatalf("kind = %q, want %q", got, btrfs.KindPreUpdate)
	}
}
