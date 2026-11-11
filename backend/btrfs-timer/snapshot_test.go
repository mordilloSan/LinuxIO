package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
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
	stubRunReplication(t, 0, nil)
	stubNotifications(t, "")
	return &kinds
}

func stubRunReplication(t *testing.T, sent int, errs []error) *int {
	t.Helper()
	old, oldPath := runReplication, btrfs.ReplicationPath
	btrfs.ReplicationPath = filepath.Join(t.TempDir(), "btrfs-replication.json")
	var runs int
	runReplication = func(context.Context, []btrfs.BtrfsReplicationJob, func(done, total int)) (int, []error) {
		runs++
		return sent, errs
	}
	t.Cleanup(func() { runReplication, btrfs.ReplicationPath = old, oldPath })
	return &runs
}

// stubNotifications keeps the notification settings and the alert store in a
// temp dir, saves report as what snapshots report ("" keeps the defaults) and
// records the alerts the pass raises.
func stubNotifications(t *testing.T, report string) *[]alerts.Observation {
	t.Helper()
	oldPath, oldRaise := alerts.Path, raiseAlert
	alerts.Path = filepath.Join(t.TempDir(), "alerts.json")
	var raised []alerts.Observation
	raiseAlert = func(_ context.Context, obs alerts.Observation) error {
		raised = append(raised, obs)
		return nil
	}
	t.Cleanup(func() { alerts.Path, raiseAlert = oldPath, oldRaise })
	if report != "" {
		if _, err := alerts.UpdateNotifications(t.Context(), func(n *alerts.NotificationSettings) error {
			n.Report[alerts.KindSnapshots] = report
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return &raised
}

// stubFailedJob makes the replication pass send sent snapshots and fail one
// job, recording its outcome as RunReplication does, which raises the job's
// alert in the store. It returns that alert's ID.
func stubFailedJob(t *testing.T, sent int) string {
	t.Helper()
	stubRunReplication(t, 0, nil) // keeps the job file in a temp dir
	job := btrfs.BtrfsReplicationJob{ID: "aaaaaaaa-0000-4000-8000-000000000001", UUID: "11111111-2222-4333-8444-555555555555", Path: "@home", Enabled: true}
	runReplication = func(ctx context.Context, _ []btrfs.BtrfsReplicationJob, _ func(done, total int)) (int, []error) {
		err := errors.New("boom")
		if recErr := btrfs.RecordReplicationOutcome(ctx, job, err); recErr != nil {
			t.Error(recErr)
		}
		return sent, []error{fmt.Errorf("%s: %w", job.ID, err)}
	}
	return alerts.ID(btrfs.AlertSource, "replication-"+job.ID)
}

// storedAlerts maps the ID of each alert in the store to its severity.
func storedAlerts(t *testing.T) map[string]string {
	t.Helper()
	doc, err := alerts.Load()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, a := range doc.Alerts {
		got[a.ID] = a.Severity
	}
	return got
}

// snapshotNotice is the notice a timeline pass at at raises with title.
func snapshotNotice(title, at string) alerts.Observation {
	return alerts.Observation{
		Source: "btrfs", Key: "snapshot-run", Severity: alerts.SeverityInfo,
		Title: title, Link: "/storage/subvolumes?view=snapshots", OccurrenceID: at,
	}
}

func TestRunSnapshotUsage(t *testing.T) {
	kinds := stubRunSnapshots(t, btrfs.RunReport{})
	for _, args := range [][]string{nil, {"bogus"}} {
		if code := runSnapshot(context.Background(), args, io.Discard, io.Discard); code != 2 {
			t.Errorf("runSnapshot(%v) = %d, want 2", args, code)
		}
	}
	if len(*kinds) != 0 {
		t.Fatalf("runner called: %v", *kinds)
	}
}

func TestRunSnapshotKindsAndExitCode(t *testing.T) {
	kinds := stubRunSnapshots(t, btrfs.RunReport{})
	if code := runSnapshot(context.Background(), []string{"run", "--now", "2026-10-19T10:00:00Z"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("run = %d, want 0", code)
	}
	if code := runSnapshot(context.Background(), []string{"pre-update"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("pre-update = %d, want 0", code)
	}
	if want := []btrfs.SnapshotKind{btrfs.KindTimeline, btrfs.KindPreUpdate}; len(*kinds) != 2 || (*kinds)[0] != want[0] || (*kinds)[1] != want[1] {
		t.Fatalf("kinds = %v, want %v", *kinds, want)
	}
	if code := runSnapshot(context.Background(), []string{"run", "--now", "yesterday"}, io.Discard, io.Discard); code != 2 {
		t.Fatalf("bad --now = %d, want 2", code)
	}

	stubRunSnapshots(t, btrfs.RunReport{Errors: []string{"u/@: boom"}})
	if code := runSnapshot(context.Background(), []string{"run"}, io.Discard, io.Discard); code != 1 {
		t.Fatalf("errors = %d, want 1", code)
	}
}

func TestRunSnapshotReplicatesAfterTimelineOnly(t *testing.T) {
	stubRunSnapshots(t, btrfs.RunReport{})
	runs := stubRunReplication(t, 0, nil)
	if code := runSnapshot(context.Background(), []string{"pre-update"}, io.Discard, io.Discard); code != 0 || *runs != 0 {
		t.Fatalf("pre-update = %d, replication runs %d; want 0, 0", code, *runs)
	}
	if code := runSnapshot(context.Background(), []string{"run"}, io.Discard, io.Discard); code != 0 || *runs != 1 {
		t.Fatalf("run = %d, replication runs %d; want 0, 1", code, *runs)
	}

	stubRunReplication(t, 0, []error{errors.New("job: boom")})
	if code := runSnapshot(context.Background(), []string{"run"}, io.Discard, io.Discard); code != 1 {
		t.Fatalf("replication errors = %d, want 1", code)
	}
}

// stubReportProgress records what the timer program reports.
func stubReportProgress(t *testing.T) *[]string {
	t.Helper()
	old := reportProgress
	var got []string
	reportProgress = func(percentage int, message string) {
		got = append(got, fmt.Sprintf("%d %s", percentage, message))
	}
	t.Cleanup(func() { reportProgress = old })
	return &got
}

func TestSnapshotPassReportsItsPhases(t *testing.T) {
	stubRunSnapshots(t, btrfs.RunReport{})
	stubRunReplication(t, 0, nil)
	runReplication = func(_ context.Context, _ []btrfs.BtrfsReplicationJob, report func(done, total int)) (int, []error) {
		report(0, 2)
		report(1, 2)
		return 0, nil
	}
	got := stubReportProgress(t)

	if code := runSnapshot(context.Background(), []string{"run"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("run = %d, want 0", code)
	}
	want := []string{"-1 Taking snapshots", "-1 Replicating 1 of 2", "-1 Replicating 2 of 2"}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("reported %q, want %q", *got, want)
	}
}

func TestSnapshotPassReportsAChange(t *testing.T) {
	stubRunSnapshots(t, btrfs.RunReport{Taken: make([]btrfs.BtrfsSnapshot, 2)})
	stubRunReplication(t, 1, nil)
	notices := stubNotifications(t, alerts.ReportChanges)
	if code := runSnapshot(context.Background(), []string{"run", "--now", "2026-11-10T10:00:00Z"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("run = %d, want 0", code)
	}
	if want := []alerts.Observation{snapshotNotice("Snapshots: 2 taken, 1 sent", "2026-11-10T10:00:00Z")}; !reflect.DeepEqual(*notices, want) {
		t.Fatalf("raised %+v, want %+v", *notices, want)
	}

	stubRunSnapshots(t, btrfs.RunReport{})
	notices = stubNotifications(t, alerts.ReportChanges)
	if code := runSnapshot(context.Background(), []string{"run"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("run = %d, want 0", code)
	}
	if len(*notices) != 0 {
		t.Fatalf("a pass with nothing to do raised %+v", *notices)
	}
}

// A pass that partly failed counts what it took and sent; the failure raises
// its own alert as before.
func TestSnapshotPassReportsWhatWorkedBesideAFailure(t *testing.T) {
	stubRunSnapshots(t, btrfs.RunReport{Taken: make([]btrfs.BtrfsSnapshot, 1)})
	notices := stubNotifications(t, alerts.ReportChanges)
	failed := stubFailedJob(t, 0)
	var stderr strings.Builder
	if code := runSnapshot(context.Background(), []string{"run", "--now", "2026-11-10T10:00:00Z"}, io.Discard, &stderr); code != 1 {
		t.Fatalf("run = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("stderr = %q, want the failure", stderr.String())
	}
	if got := storedAlerts(t); got[failed] != alerts.SeverityError {
		t.Fatalf("alerts = %v, want the failure alert %s", got, failed)
	}
	if want := []alerts.Observation{snapshotNotice("Snapshots: 1 taken, 0 sent", "2026-11-10T10:00:00Z")}; !reflect.DeepEqual(*notices, want) {
		t.Fatalf("raised %+v, want %+v", *notices, want)
	}

	// A failed rule beside a snapshot another rule took.
	stubRunSnapshots(t, btrfs.RunReport{Taken: make([]btrfs.BtrfsSnapshot, 1), Errors: []string{"u/@: boom"}})
	notices = stubNotifications(t, alerts.ReportChanges)
	stderr.Reset()
	if code := runSnapshot(context.Background(), []string{"run", "--now", "2026-11-10T11:00:00Z"}, io.Discard, &stderr); code != 1 {
		t.Fatalf("run = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "u/@: boom") {
		t.Fatalf("stderr = %q, want the failed rule", stderr.String())
	}
	if want := []alerts.Observation{snapshotNotice("Snapshots: 1 taken, 0 sent", "2026-11-10T11:00:00Z")}; !reflect.DeepEqual(*notices, want) {
		t.Fatalf("raised %+v, want %+v", *notices, want)
	}
}

// A pass that failed and took and sent nothing raises no notice, even under
// every_run: its failures have their own alerts.
func TestSnapshotPassReportsNoNoticeBesideFailuresAlone(t *testing.T) {
	stubRunSnapshots(t, btrfs.RunReport{Errors: []string{"u/@: boom"}})
	notices := stubNotifications(t, alerts.ReportEveryRun)
	var stderr strings.Builder
	if code := runSnapshot(context.Background(), []string{"run"}, io.Discard, &stderr); code != 1 {
		t.Fatalf("failed rule: run = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "u/@: boom") {
		t.Fatalf("stderr = %q, want the failed rule", stderr.String())
	}
	if len(*notices) != 0 {
		t.Fatalf("failed rule: raised %+v, want none", *notices)
	}

	// Replication jobs that cannot be loaded.
	stubRunSnapshots(t, btrfs.RunReport{})
	notices = stubNotifications(t, alerts.ReportEveryRun)
	if err := os.WriteFile(btrfs.ReplicationPath, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := runSnapshot(context.Background(), []string{"run"}, io.Discard, &stderr); code != 1 {
		t.Fatalf("unreadable jobs: run = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "load replication jobs") {
		t.Fatalf("stderr = %q, want the jobs that could not be loaded", stderr.String())
	}
	if len(*notices) != 0 {
		t.Fatalf("unreadable jobs: raised %+v, want none", *notices)
	}
}

func TestSnapshotPassReportsEveryRun(t *testing.T) {
	stubRunSnapshots(t, btrfs.RunReport{})
	notices := stubNotifications(t, alerts.ReportEveryRun)
	for _, at := range []string{"2026-11-10T10:00:00Z", "2026-11-10T11:00:00Z"} {
		if code := runSnapshot(context.Background(), []string{"run", "--now", at}, io.Discard, io.Discard); code != 0 {
			t.Fatalf("run = %d, want 0", code)
		}
	}
	// Each pass is a new occurrence of the one notice.
	want := []alerts.Observation{
		snapshotNotice("Snapshots: nothing to do", "2026-11-10T10:00:00Z"),
		snapshotNotice("Snapshots: nothing to do", "2026-11-10T11:00:00Z"),
	}
	if !reflect.DeepEqual(*notices, want) {
		t.Fatalf("raised %+v, want %+v", *notices, want)
	}

	// A pre-update pass is not the snapshot unit's run, and a stopped pass
	// reports nothing.
	*notices = nil
	if code := runSnapshot(context.Background(), []string{"pre-update"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("pre-update = %d, want 0", code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stubRunReplication(t, 0, []error{context.Canceled}) // as RunReplication stops
	if code := runSnapshot(ctx, []string{"run"}, io.Discard, io.Discard); code != 1 {
		t.Fatalf("stopped run = %d, want 1", code)
	}
	if len(*notices) != 0 {
		t.Fatalf("raised %+v, want none", *notices)
	}
}

func TestSnapshotPassReportsOnlyFailuresByDefault(t *testing.T) {
	for _, report := range []string{"", alerts.ReportFailures} {
		stubRunSnapshots(t, btrfs.RunReport{Taken: make([]btrfs.BtrfsSnapshot, 2)})
		notices := stubNotifications(t, report)
		failed := stubFailedJob(t, 1)
		if code := runSnapshot(context.Background(), []string{"run"}, io.Discard, io.Discard); code != 1 {
			t.Fatalf("report %q: run = %d, want 1", report, code)
		}
		if len(*notices) != 0 {
			t.Fatalf("report %q: raised %+v, want none", report, *notices)
		}
		if got := storedAlerts(t); got[failed] != alerts.SeverityError {
			t.Fatalf("report %q: alerts = %v, want the failure alert %s", report, got, failed)
		}
	}
}
