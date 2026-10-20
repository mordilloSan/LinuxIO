package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

func stubRunSnapshots(t *testing.T, report btrfs.RunReport) *[]btrfs.SnapshotKind {
	t.Helper()
	old, oldPath := runSnapshots, btrfs.RulesPath
	btrfs.RulesPath = filepath.Join(t.TempDir(), "btrfs-snapshots.json")
	var kinds []btrfs.SnapshotKind
	runSnapshots = func(_ context.Context, _ []btrfs.BtrfsSnapshotRule, _ time.Time, kind btrfs.SnapshotKind) (btrfs.RunReport, error) {
		kinds = append(kinds, kind)
		return report, nil
	}
	t.Cleanup(func() { runSnapshots, btrfs.RulesPath = old, oldPath })
	stubRunReplication(t, nil)
	return &kinds
}

func stubRunReplication(t *testing.T, errs []error) *int {
	t.Helper()
	old, oldPath := runReplication, btrfs.ReplicationPath
	btrfs.ReplicationPath = filepath.Join(t.TempDir(), "btrfs-replication.json")
	var runs int
	runReplication = func(context.Context, []btrfs.BtrfsReplicationJob) []error {
		runs++
		return errs
	}
	t.Cleanup(func() { runReplication, btrfs.ReplicationPath = old, oldPath })
	return &runs
}

func TestRunBtrfsSnapshotUsage(t *testing.T) {
	kinds := stubRunSnapshots(t, btrfs.RunReport{})
	for _, args := range [][]string{nil, {"bogus"}} {
		if code := runBtrfsSnapshot(context.Background(), args); code != 2 {
			t.Errorf("runBtrfsSnapshot(%v) = %d, want 2", args, code)
		}
	}
	if len(*kinds) != 0 {
		t.Fatalf("runner called: %v", *kinds)
	}
}

func TestRunBtrfsSnapshotKindsAndExitCode(t *testing.T) {
	kinds := stubRunSnapshots(t, btrfs.RunReport{})
	if code := runBtrfsSnapshot(context.Background(), []string{"run", "--now", "2026-10-19T10:00:00Z"}); code != 0 {
		t.Fatalf("run = %d, want 0", code)
	}
	if code := runBtrfsSnapshot(context.Background(), []string{"pre-update"}); code != 0 {
		t.Fatalf("pre-update = %d, want 0", code)
	}
	if want := []btrfs.SnapshotKind{btrfs.KindTimeline, btrfs.KindPreUpdate}; len(*kinds) != 2 || (*kinds)[0] != want[0] || (*kinds)[1] != want[1] {
		t.Fatalf("kinds = %v, want %v", *kinds, want)
	}
	if code := runBtrfsSnapshot(context.Background(), []string{"run", "--now", "yesterday"}); code != 2 {
		t.Fatalf("bad --now = %d, want 2", code)
	}

	stubRunSnapshots(t, btrfs.RunReport{Errors: []string{"u/@: boom"}})
	if code := runBtrfsSnapshot(context.Background(), []string{"run"}); code != 1 {
		t.Fatalf("errors = %d, want 1", code)
	}
}

func TestRunBtrfsSnapshotReplicatesAfterTimelineOnly(t *testing.T) {
	stubRunSnapshots(t, btrfs.RunReport{})
	runs := stubRunReplication(t, nil)
	if code := runBtrfsSnapshot(context.Background(), []string{"pre-update"}); code != 0 || *runs != 0 {
		t.Fatalf("pre-update = %d, replication runs %d; want 0, 0", code, *runs)
	}
	if code := runBtrfsSnapshot(context.Background(), []string{"run"}); code != 0 || *runs != 1 {
		t.Fatalf("run = %d, replication runs %d; want 0, 1", code, *runs)
	}

	stubRunReplication(t, []error{errors.New("job: boom")})
	if code := runBtrfsSnapshot(context.Background(), []string{"run"}); code != 1 {
		t.Fatalf("replication errors = %d, want 1", code)
	}
}
