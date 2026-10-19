package storage

import (
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

func TestWithStoredRunStateIgnoresClient(t *testing.T) {
	ran := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	stored, forged := "stored", "forged"
	jobs := []btrfs.BtrfsReplicationJob{{ID: "a", LastRun: &ran, LastError: &stored}}
	forgedAt := ran.Add(time.Hour)

	got := withStoredRunState(jobs, btrfs.BtrfsReplicationJob{ID: "a", Enabled: true, LastRun: &forgedAt, LastError: &forged})
	if !got.Enabled || got.LastRun != &ran || got.LastError != &stored {
		t.Fatalf("existing job = %+v, want stored run state", got)
	}
	got = withStoredRunState(jobs, btrfs.BtrfsReplicationJob{ID: "b", LastRun: &forgedAt, LastError: &forged})
	if got.LastRun != nil || got.LastError != nil {
		t.Fatalf("new job = %+v, want no run state", got)
	}
}
