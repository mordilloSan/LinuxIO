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
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
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
	stubStartReplication(t, nil)
	stubNotifications(t, "")
	return &kinds
}

// stubStartReplication keeps the job file in a temp dir and counts the starts
// of the replication unit, each returning err.
func stubStartReplication(t *testing.T, err error) *int {
	t.Helper()
	old, oldPath := startReplication, btrfs.ReplicationPath
	btrfs.ReplicationPath = filepath.Join(t.TempDir(), "btrfs-replication.json")
	var starts int
	startReplication = func(context.Context) error {
		starts++
		return err
	}
	t.Cleanup(func() { startReplication, btrfs.ReplicationPath = old, oldPath })
	return &starts
}

// saveJobs writes jobs to the job file.
func saveJobs(t *testing.T, jobs ...btrfs.BtrfsReplicationJob) {
	t.Helper()
	if _, err := btrfs.UpdateJobs(t.Context(), func(j *[]btrfs.BtrfsReplicationJob) error {
		*j = jobs
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

var testJob = btrfs.BtrfsReplicationJob{ID: "aaaaaaaa-0000-4000-8000-000000000001", UUID: "11111111-2222-4333-8444-555555555555", Path: "@home", Enabled: true}

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

	// A failed rule raised its own alert: the timer's pass says so with its
	// exit status, and the pre-update pass, which no unit hook follows, keeps 1.
	stubRunSnapshots(t, btrfs.RunReport{Errors: []string{"u/@: boom"}})
	if code := runSnapshot(context.Background(), []string{"run"}, io.Discard, io.Discard); code != scheduledrun.ExitReported {
		t.Fatalf("errors = %d, want %d", code, scheduledrun.ExitReported)
	}
	if code := runSnapshot(context.Background(), []string{"pre-update"}, io.Discard, io.Discard); code != 1 {
		t.Fatalf("pre-update errors = %d, want 1", code)
	}
}

// A timeline pass starts the replication unit when a job is enabled; the
// pre-update pass never does.
func TestRunSnapshotStartsReplicationAfterTimelineOnly(t *testing.T) {
	stubRunSnapshots(t, btrfs.RunReport{})
	starts := stubStartReplication(t, nil)
	var stdout strings.Builder
	if code := runSnapshot(context.Background(), []string{"run"}, &stdout, io.Discard); code != 0 || *starts != 0 {
		t.Fatalf("no jobs: run = %d, starts %d; want 0, 0", code, *starts)
	}
	if !strings.Contains(stdout.String(), "replication_started=false") {
		t.Fatalf("no jobs: stdout = %q", stdout.String())
	}
	disabled := testJob
	disabled.Enabled = false
	saveJobs(t, disabled)
	if code := runSnapshot(context.Background(), []string{"run"}, io.Discard, io.Discard); code != 0 || *starts != 0 {
		t.Fatalf("disabled job: run = %d, starts %d; want 0, 0", code, *starts)
	}

	saveJobs(t, disabled, testJob)
	if code := runSnapshot(context.Background(), []string{"pre-update"}, io.Discard, io.Discard); code != 0 || *starts != 0 {
		t.Fatalf("pre-update = %d, starts %d; want 0, 0", code, *starts)
	}
	stdout.Reset()
	if code := runSnapshot(context.Background(), []string{"run"}, &stdout, io.Discard); code != 0 || *starts != 1 {
		t.Fatalf("run = %d, starts %d; want 0, 1", code, *starts)
	}
	if !strings.Contains(stdout.String(), "replication_started=true") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

// A replication that could not be started raised no alert of its own, so the
// pass exits 1 for the unit's run alert, even beside a failed rule.
func TestRunSnapshotReportsAReplicationThatDidNotStart(t *testing.T) {
	stubRunSnapshots(t, btrfs.RunReport{Errors: []string{"u/@: boom"}})
	stubStartReplication(t, errors.New("start unit linuxio-btrfs-replicate.service: access denied"))
	saveJobs(t, testJob)
	var stderr strings.Builder
	if code := runSnapshot(context.Background(), []string{"run"}, io.Discard, &stderr); code != 1 {
		t.Fatalf("run = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "start btrfs replication: start unit linuxio-btrfs-replicate.service: access denied") || !strings.Contains(stderr.String(), "u/@: boom") {
		t.Fatalf("stderr = %q, want both failures", stderr.String())
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

func TestSnapshotPassReportsItsPhase(t *testing.T) {
	stubRunSnapshots(t, btrfs.RunReport{})
	saveJobs(t, testJob)
	got := stubReportProgress(t)

	if code := runSnapshot(context.Background(), []string{"run"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("run = %d, want 0", code)
	}
	if want := []string{"-1 Taking snapshots"}; !reflect.DeepEqual(*got, want) {
		t.Fatalf("reported %q, want %q", *got, want)
	}
}

func TestSnapshotPassReportsAChange(t *testing.T) {
	stubRunSnapshots(t, btrfs.RunReport{Taken: make([]btrfs.BtrfsSnapshot, 2)})
	notices := stubNotifications(t, alerts.ReportChanges)
	if code := runSnapshot(context.Background(), []string{"run", "--now", "2026-11-10T10:00:00Z"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("run = %d, want 0", code)
	}
	if want := []alerts.Observation{snapshotNotice("Snapshots: 2 taken", "2026-11-10T10:00:00Z")}; !reflect.DeepEqual(*notices, want) {
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

// A pass that partly failed counts what it took; the failure raises its own
// alert as before.
func TestSnapshotPassReportsWhatWorkedBesideAFailure(t *testing.T) {
	stubRunSnapshots(t, btrfs.RunReport{Taken: make([]btrfs.BtrfsSnapshot, 1), Errors: []string{"u/@: boom"}})
	notices := stubNotifications(t, alerts.ReportChanges)
	var stderr strings.Builder
	if code := runSnapshot(context.Background(), []string{"run", "--now", "2026-11-10T11:00:00Z"}, io.Discard, &stderr); code != scheduledrun.ExitReported {
		t.Fatalf("run = %d, want %d", code, scheduledrun.ExitReported)
	}
	if !strings.Contains(stderr.String(), "u/@: boom") {
		t.Fatalf("stderr = %q, want the failed rule", stderr.String())
	}
	if want := []alerts.Observation{snapshotNotice("Snapshots: 1 taken", "2026-11-10T11:00:00Z")}; !reflect.DeepEqual(*notices, want) {
		t.Fatalf("raised %+v, want %+v", *notices, want)
	}
}

// A pass that failed and took nothing raises no notice, even under
// every_run: its failures have their own alerts.
func TestSnapshotPassReportsNoNoticeBesideFailuresAlone(t *testing.T) {
	stubRunSnapshots(t, btrfs.RunReport{Errors: []string{"u/@: boom"}})
	notices := stubNotifications(t, alerts.ReportEveryRun)
	var stderr strings.Builder
	if code := runSnapshot(context.Background(), []string{"run"}, io.Discard, &stderr); code != scheduledrun.ExitReported {
		t.Fatalf("failed rule: run = %d, want %d", code, scheduledrun.ExitReported)
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
	// reports nothing and starts no replication.
	*notices = nil
	if code := runSnapshot(context.Background(), []string{"pre-update"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("pre-update = %d, want 0", code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	starts := stubStartReplication(t, nil)
	saveJobs(t, testJob)
	if code := runSnapshot(ctx, []string{"run"}, io.Discard, io.Discard); code != 1 {
		t.Fatalf("stopped run = %d, want 1", code)
	}
	if len(*notices) != 0 || *starts != 0 {
		t.Fatalf("raised %+v, started %d; want none", *notices, *starts)
	}
}

func TestSnapshotPassReportsOnlyFailuresByDefault(t *testing.T) {
	for _, report := range []string{"", alerts.ReportFailures} {
		stubRunSnapshots(t, btrfs.RunReport{Taken: make([]btrfs.BtrfsSnapshot, 2)})
		notices := stubNotifications(t, report)
		if code := runSnapshot(context.Background(), []string{"run"}, io.Discard, io.Discard); code != 0 {
			t.Fatalf("report %q: run = %d, want 0", report, code)
		}
		if len(*notices) != 0 {
			t.Fatalf("report %q: raised %+v, want none", report, *notices)
		}
	}
}
