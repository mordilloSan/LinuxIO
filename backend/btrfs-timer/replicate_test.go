package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

// stubRunReplication keeps the job file and the alert store in temp dirs and
// makes the replication run return replicated, sent and errs. It returns the
// number of runs.
func stubRunReplication(t *testing.T, replicated, sent int, errs []error) *int {
	t.Helper()
	stubStartReplication(t, nil) // keeps the job file in a temp dir
	stubNotifications(t, "")
	old := runReplication
	var runs int
	runReplication = func(context.Context, []btrfs.BtrfsReplicationJob, func(done, total int)) (int, int, []error) {
		runs++
		return replicated, sent, errs
	}
	t.Cleanup(func() { runReplication = old })
	return &runs
}

// stubFailedJob makes the replication run send sent snapshots and fail one
// job, recording its outcome as RunReplication does, which raises the job's
// alert in the store. It returns that alert's ID.
func stubFailedJob(t *testing.T, sent int) string {
	t.Helper()
	runReplication = func(ctx context.Context, _ []btrfs.BtrfsReplicationJob, _ func(done, total int)) (int, int, []error) {
		err := errors.New("boom")
		if recErr := btrfs.RecordReplicationOutcome(ctx, testJob, err); recErr != nil {
			t.Error(recErr)
		}
		return sent, sent, []error{scheduledrun.Reported(fmt.Errorf("%s: %w", testJob.ID, err))}
	}
	return alerts.ID(btrfs.AlertSource, "replication-"+testJob.ID)
}

// replicationNotice is the notice a replication run at at raises with title.
func replicationNotice(title, at string) alerts.Observation {
	return alerts.Observation{
		Source: "btrfs", Key: "replication-run", Severity: alerts.SeverityInfo,
		Title: title, Link: "/storage/subvolumes?view=replication", OccurrenceID: at,
	}
}

func TestRunReplicateUsage(t *testing.T) {
	runs := stubRunReplication(t, 0, 0, nil)
	for _, args := range [][]string{nil, {"bogus"}, {"run", "--now", "yesterday"}, {"run", "extra", "--bogus"}} {
		if code := runCLI(context.Background(), append([]string{"replicate"}, args...), io.Discard, io.Discard); code != 2 {
			t.Errorf("replicate %v = %d, want 2", args, code)
		}
	}
	if *runs != 0 {
		t.Fatalf("replication ran on bad usage: %d", *runs)
	}
	if code := runCLI(context.Background(), []string{"replicate", "run"}, io.Discard, io.Discard); code != 0 || *runs != 1 {
		t.Fatalf("replicate run = %d, runs %d; want 0, 1", code, *runs)
	}
}

// The exit status keeps the contract of the unit's end hook: 0 for a clean
// run, ExitReported when each failure raised its job's alert, 1 for a failure
// without one, such as an outcome that could not be recorded.
func TestReplicatePassExitStatus(t *testing.T) {
	reported := scheduledrun.Reported(errors.New("job: boom"))
	for name, tt := range map[string]struct {
		errs []error
		want int
	}{
		"clean":                {nil, 0},
		"failed jobs reported": {[]error{reported}, scheduledrun.ExitReported},
		"outcome not recorded": {[]error{reported, errors.New("job: record outcome: disk full")}, 1},
		"stopped":              {[]error{context.Canceled}, 1},
	} {
		stubRunReplication(t, 0, 0, tt.errs)
		if code := runReplicate(context.Background(), []string{"run"}, io.Discard, io.Discard); code != tt.want {
			t.Errorf("%s: replicate = %d, want %d", name, code, tt.want)
		}
	}

	stubRunReplication(t, 0, 0, nil)
	if err := os.WriteFile(btrfs.ReplicationPath, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	if code := runReplicate(context.Background(), []string{"run"}, io.Discard, &stderr); code != 1 {
		t.Fatalf("unreadable jobs: replicate = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "Failed to load btrfs replication jobs") {
		t.Fatalf("stderr = %q, want the jobs that could not be loaded", stderr.String())
	}
}

// A job that sent its snapshot but whose outcome could not be recorded counts
// in replicated and sent beside its error.
func TestReplicatePassSummary(t *testing.T) {
	stubRunReplication(t, 1, 1, []error{errors.New("job: record outcome: disk full")})
	var stdout, stderr strings.Builder
	if code := runReplicate(context.Background(), []string{"run"}, &stdout, &stderr); code != 1 {
		t.Fatalf("replicate = %d, want 1", code)
	}
	if got, want := stdout.String(), "replicated=1 sent=1 errors=1\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if !strings.Contains(stderr.String(), "record outcome: disk full") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestReplicatePassReportsEachJob(t *testing.T) {
	stubRunReplication(t, 0, 0, nil)
	runReplication = func(_ context.Context, _ []btrfs.BtrfsReplicationJob, report func(done, total int)) (int, int, []error) {
		report(0, 2)
		report(1, 2)
		return 2, 0, nil
	}
	got := stubReportProgress(t)
	if code := runReplicate(context.Background(), []string{"run"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("replicate = %d, want 0", code)
	}
	if want := []string{"-1 Replicating 1 of 2", "-1 Replicating 2 of 2"}; !reflect.DeepEqual(*got, want) {
		t.Fatalf("reported %q, want %q", *got, want)
	}
}

func TestReplicatePassNotices(t *testing.T) {
	stubRunReplication(t, 1, 1, nil)
	notices := stubNotifications(t, alerts.ReportChanges)
	if code := runReplicate(context.Background(), []string{"run", "--now", "2026-11-10T10:00:00Z"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("replicate = %d, want 0", code)
	}
	if want := []alerts.Observation{replicationNotice("Replication: 1 sent", "2026-11-10T10:00:00Z")}; !reflect.DeepEqual(*notices, want) {
		t.Fatalf("changes: raised %+v, want %+v", *notices, want)
	}

	// Up to date: a change only under every_run.
	stubRunReplication(t, 1, 0, nil)
	notices = stubNotifications(t, alerts.ReportChanges)
	if code := runReplicate(context.Background(), []string{"run"}, io.Discard, io.Discard); code != 0 || len(*notices) != 0 {
		t.Fatalf("changes, nothing sent: replicate = %d, raised %+v; want 0, none", code, *notices)
	}
	notices = stubNotifications(t, alerts.ReportEveryRun)
	if code := runReplicate(context.Background(), []string{"run", "--now", "2026-11-10T11:00:00Z"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("replicate = %d, want 0", code)
	}
	if want := []alerts.Observation{replicationNotice("Replication: nothing to do", "2026-11-10T11:00:00Z")}; !reflect.DeepEqual(*notices, want) {
		t.Fatalf("every_run: raised %+v, want %+v", *notices, want)
	}

	// A stopped run reports nothing.
	stubRunReplication(t, 0, 0, []error{context.Canceled})
	notices = stubNotifications(t, alerts.ReportEveryRun)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := runReplicate(ctx, []string{"run"}, io.Discard, io.Discard); code != 1 || len(*notices) != 0 {
		t.Fatalf("stopped: replicate = %d, raised %+v; want 1, none", code, *notices)
	}
}

// A run whose every enabled job another run held did no work: it exits
// ExitSkipped, so its end hook changes no alert, and it raises no notice.
func TestReplicatePassSkippedWhenEveryJobIsHeld(t *testing.T) {
	stubRunReplication(t, 0, 0, nil)
	notices := stubNotifications(t, alerts.ReportEveryRun)
	saveJobs(t, testJob)
	if code := runReplicate(context.Background(), []string{"run"}, io.Discard, io.Discard); code != scheduledrun.ExitSkipped || len(*notices) != 0 {
		t.Fatalf("every job held: replicate = %d, raised %+v; want %d, none", code, *notices, scheduledrun.ExitSkipped)
	}
}

// A failed job raises its own alert and exits ExitReported; what the run sent
// beside it still counts, and a run that failed and sent nothing raises no
// notice even under every_run.
func TestReplicatePassReportsAFailedJob(t *testing.T) {
	for _, tt := range []struct {
		report string
		sent   int
		want   []alerts.Observation
	}{
		{alerts.ReportChanges, 1, []alerts.Observation{replicationNotice("Replication: 1 sent", "2026-11-10T10:00:00Z")}},
		{alerts.ReportEveryRun, 0, nil},
		{"", 1, nil},
		{alerts.ReportFailures, 1, nil},
	} {
		stubRunReplication(t, 0, 0, nil)
		notices := stubNotifications(t, tt.report)
		failed := stubFailedJob(t, tt.sent)
		var stderr strings.Builder
		if code := runReplicate(context.Background(), []string{"run", "--now", "2026-11-10T10:00:00Z"}, io.Discard, &stderr); code != scheduledrun.ExitReported {
			t.Fatalf("report %q: replicate = %d, want %d", tt.report, code, scheduledrun.ExitReported)
		}
		if !strings.Contains(stderr.String(), "boom") {
			t.Fatalf("report %q: stderr = %q, want the failure", tt.report, stderr.String())
		}
		if got := storedAlerts(t); got[failed] != alerts.SeverityError {
			t.Fatalf("report %q: alerts = %v, want the failure alert %s", tt.report, got, failed)
		}
		if !reflect.DeepEqual(*notices, tt.want) {
			t.Fatalf("report %q: raised %+v, want %+v", tt.report, *notices, tt.want)
		}
	}
}
