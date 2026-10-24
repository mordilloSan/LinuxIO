package btrfs

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
)

func useTempAlerts(t *testing.T) {
	t.Helper()
	old, oldRules := alerts.Path, RulesPath
	alerts.Path = filepath.Join(t.TempDir(), "alerts.json")
	RulesPath = filepath.Join(t.TempDir(), "btrfs-snapshots.json") // the per-filesystem run locks live beside it
	t.Cleanup(func() { alerts.Path, RulesPath = old, oldRules })
}

func TestRunSkipsMissingFilesystem(t *testing.T) {
	useTempAlerts(t)
	stubMounts(t, []Mount{{Mountpoint: "/other", UUID: dataUUID, SubvolID: 5}})
	stubSubvolOps(t, func(args []string) error {
		t.Errorf("unexpected btrfs %v", args)
		return nil
	})

	rules := []BtrfsSnapshotRule{
		{UUID: rootUUID, Path: "data/Photos", Enabled: true, Every: EveryHourly},
		{UUID: rootUUID, Path: "@", Enabled: false, Every: EveryHourly},
	}
	report, err := Run(context.Background(), rules, snapshotTime, KindTimeline)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []string{rootUUID + "/data/Photos"}; !reflect.DeepEqual(report.Skipped, want) {
		t.Fatalf("Skipped = %v, want %v", report.Skipped, want)
	}
	if len(report.Taken) != 0 || len(report.Errors) != 0 {
		t.Fatalf("report = %+v", report)
	}
	doc, err := alerts.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Alerts) != 1 {
		t.Fatalf("alerts = %+v, want one", doc.Alerts)
	}
	a := doc.Alerts[0]
	if a.ID != "btrfs/snapshot-3f1c0a52-data-photos" || a.Severity != alerts.SeverityWarning ||
		a.Link != "/storage/snapshots?fs="+rootUUID || !strings.Contains(a.Title, "data/Photos") {
		t.Fatalf("alert = %+v", a)
	}
}

// stubRunnerBtrfs serves a subvolume listing that grows on snapshot and
// shrinks on delete, recording every mutation.
func stubRunnerBtrfs(t *testing.T, top string, rows []subvolumeRow) *[][]string {
	t.Helper()
	var calls [][]string
	stubSubvolumes(t, nil, 256, &calls)
	readSubvolumes = func(string) ([]subvolumeRow, int, error) { return append(fixtureRows(), rows...), 256, nil }
	stubSubvolOps(t, func(args []string) error {
		joined := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(joined, "subvolume snapshot -r "):
			rows = append(rows, snapshotSubvolume(100+len(calls), strings.TrimPrefix(args[4], top+"/")))
		case strings.HasPrefix(joined, "subvolume delete "):
			rel := strings.TrimPrefix(args[2], top+"/")
			rows = slices.DeleteFunc(rows, func(r subvolumeRow) bool { return r.Path == rel })
		}
		calls = append(calls, args)
		return nil
	})
	return &calls
}

func TestRunReportsRuleAlreadyRunning(t *testing.T) {
	useTempAlerts(t)
	top := t.TempDir()
	stubMounts(t, []Mount{{Mountpoint: top, UUID: rootUUID, SubvolID: 5}})
	calls := stubRunnerBtrfs(t, top, nil)
	release, err := filelock.AcquireExclusive(context.Background(), RulesPath+"."+rootUUID+".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = release() }()
	oldWait := snapshotLockWait
	snapshotLockWait = 50 * time.Millisecond
	t.Cleanup(func() { snapshotLockWait = oldWait })

	rules := []BtrfsSnapshotRule{{UUID: rootUUID, Path: "@", Enabled: true, Every: EveryHourly}}
	report, err := Run(context.Background(), rules, snapshotTime, KindTimeline)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Errors) != 1 || !strings.Contains(report.Errors[0], "already running") || len(report.Taken) != 0 || len(*calls) != 0 {
		t.Fatalf("report = %+v, calls = %v", report, *calls)
	}
}

// A run that overlaps another holding the filesystem lock waits for it
// instead of failing, so a pre-update snapshot is not lost to the timer.
func TestRunWaitsForRuleLock(t *testing.T) {
	useTempAlerts(t)
	top := t.TempDir()
	stubMounts(t, []Mount{{Mountpoint: top, UUID: rootUUID, SubvolID: 5}})
	stubRunnerBtrfs(t, top, nil)
	release, err := filelock.AcquireExclusive(context.Background(), RulesPath+"."+rootUUID+".lock")
	if err != nil {
		t.Fatal(err)
	}
	released := time.AfterFunc(300*time.Millisecond, func() { _ = release() })
	defer released.Stop()

	rules := []BtrfsSnapshotRule{{UUID: rootUUID, Path: "@", BeforeUpdates: true}}
	report, err := Run(context.Background(), rules, snapshotTime, KindPreUpdate)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Errors) != 0 || len(report.Taken) != 1 {
		t.Fatalf("report = %+v, want one snapshot and no errors", report)
	}
}

func TestRunTakesDueOnly(t *testing.T) {
	useTempAlerts(t)
	top := t.TempDir()
	stubMounts(t, []Mount{{Mountpoint: top, UUID: rootUUID, SubvolID: 5}})
	now := snapshotTime
	snap := func(source string, ago time.Duration) string {
		return SnapshotPath(source, now.Add(-ago), KindTimeline)
	}
	rows := []subvolumeRow{
		snapshotSubvolume(1, snap("@", 4*time.Hour)),
		snapshotSubvolume(2, snap("@", 3*time.Hour)),
		snapshotSubvolume(3, snap("@", 2*time.Hour)),
		snapshotSubvolume(4, snap("@home", time.Hour)),
	}
	calls := stubRunnerBtrfs(t, top, rows)

	rules := []BtrfsSnapshotRule{
		{UUID: rootUUID, Path: "@", Enabled: true, Every: EveryHourly, Keep: BtrfsKeep{Hourly: 2}},
		{UUID: rootUUID, Path: "@home", Enabled: true, Every: EveryDaily, Keep: BtrfsKeep{Daily: 5}},
	}
	report, err := Run(context.Background(), rules, now, KindTimeline)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := [][]string{
		{"subvolume", "snapshot", "-r", filepath.Join(top, "@"), filepath.Join(top, SnapshotPath("@", now, KindTimeline))},
		{"subvolume", "delete", filepath.Join(top, snap("@", 4*time.Hour))},
		{"subvolume", "delete", filepath.Join(top, snap("@", 3*time.Hour))},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("btrfs calls =\n%v\nwant\n%v", *calls, want)
	}
	if len(report.Taken) != 1 || report.Taken[0].Source != "@" || len(report.Pruned) != 2 ||
		len(report.Skipped) != 0 || len(report.Errors) != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestRunPreUpdateOnlyBeforeUpdatesRules(t *testing.T) {
	useTempAlerts(t)
	top := t.TempDir()
	stubMounts(t, []Mount{{Mountpoint: top, UUID: rootUUID, SubvolID: 5}})
	calls := stubRunnerBtrfs(t, top, nil)

	rules := []BtrfsSnapshotRule{
		{UUID: rootUUID, Path: "@", Enabled: true, Every: EveryHourly, BeforeUpdates: true},
		{UUID: rootUUID, Path: "@home", Enabled: true, Every: EveryHourly},
	}
	report, err := Run(context.Background(), rules, snapshotTime, KindPreUpdate)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(*calls) != 1 || len(report.Taken) != 1 || report.Taken[0].Kind != KindPreUpdate {
		t.Fatalf("calls = %v, report = %+v", *calls, report)
	}
}

// A disabled rule still snapshots before updates, but prunes only its
// pre-update snapshots: its timeline is left alone.
func TestRunPreUpdateDisabledRuleKeepsTimeline(t *testing.T) {
	useTempAlerts(t)
	top := t.TempDir()
	stubMounts(t, []Mount{{Mountpoint: top, UUID: rootUUID, SubvolID: 5}})
	now := snapshotTime
	var rows []subvolumeRow
	for i := range 4 {
		rows = append(rows, snapshotSubvolume(10+i, SnapshotPath("@", now.Add(-time.Duration(i+1)*time.Hour), KindTimeline)))
	}
	for i := range 3 {
		rows = append(rows, snapshotSubvolume(20+i, SnapshotPath("@", now.Add(-time.Duration(i+1)*24*time.Hour), KindPreUpdate)))
	}
	calls := stubRunnerBtrfs(t, top, rows)

	rules := []BtrfsSnapshotRule{{UUID: rootUUID, Path: "@", Enabled: false, Every: EveryHourly, BeforeUpdates: true}}
	report, err := Run(context.Background(), rules, now, KindPreUpdate)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := [][]string{
		{"subvolume", "snapshot", "-r", filepath.Join(top, "@"), filepath.Join(top, SnapshotPath("@", now, KindPreUpdate))},
		{"subvolume", "delete", filepath.Join(top, SnapshotPath("@", now.Add(-72*time.Hour), KindPreUpdate))},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("btrfs calls =\n%v\nwant\n%v", *calls, want)
	}
	if len(report.Taken) != 1 || len(report.Pruned) != 1 || report.Pruned[0].Kind != KindPreUpdate {
		t.Fatalf("report = %+v", report)
	}
}

func TestRunCollectsPerRuleErrors(t *testing.T) {
	useTempAlerts(t)
	top := t.TempDir()
	stubMounts(t, []Mount{{Mountpoint: top, UUID: rootUUID, SubvolID: 5}})
	calls := stubRunnerBtrfs(t, top, nil)

	rules := []BtrfsSnapshotRule{
		{UUID: rootUUID, Path: "missing", Enabled: true, Every: EveryHourly},
		{UUID: rootUUID, Path: "@", Enabled: true, Every: EveryHourly, Keep: BtrfsKeep{Hourly: 1}},
	}
	report, err := Run(context.Background(), rules, snapshotTime, KindTimeline)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Errors) != 1 || !strings.HasPrefix(report.Errors[0], rootUUID+"/missing: ") || len(report.Taken) != 1 || len(*calls) != 1 {
		t.Fatalf("report = %+v, calls = %v", report, *calls)
	}
	doc, _ := alerts.Load()
	if len(doc.Alerts) != 1 || doc.Alerts[0].Severity != alerts.SeverityError {
		t.Fatalf("alerts = %+v", doc.Alerts)
	}
}

// One snapshot that cannot be deleted must not stop the newer ones being
// pruned; the failure still reaches the report and the alert.
func TestRunPruneContinuesPastUndeletableSnapshot(t *testing.T) {
	useTempAlerts(t)
	top := t.TempDir()
	stubMounts(t, []Mount{{Mountpoint: top, UUID: rootUUID, SubvolID: 5}})
	now := snapshotTime
	snap := func(ago time.Duration) string { return SnapshotPath("@", now.Add(-ago), KindTimeline) }
	rows := []subvolumeRow{
		snapshotSubvolume(1, snap(5*time.Hour)),
		snapshotSubvolume(2, snap(4*time.Hour)),
		snapshotSubvolume(3, snap(3*time.Hour)),
	}
	stubRunnerBtrfs(t, top, rows)
	destroy := subvolOps.destroy
	subvolOps.destroy = func(p string) error {
		if p == filepath.Join(top, snap(5*time.Hour)) {
			return errors.New("busy")
		}
		return destroy(p)
	}

	rules := []BtrfsSnapshotRule{{UUID: rootUUID, Path: "@", Enabled: true, Every: EveryHourly, Keep: BtrfsKeep{Hourly: 1}}}
	report, err := Run(context.Background(), rules, now, KindTimeline)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Pruned) != 2 || report.Pruned[0].Path != snap(4*time.Hour) || report.Pruned[1].Path != snap(3*time.Hour) {
		t.Fatalf("Pruned = %+v", report.Pruned)
	}
	if len(report.Errors) != 1 || !strings.Contains(report.Errors[0], snap(5*time.Hour)) || !strings.Contains(report.Errors[0], "busy") {
		t.Fatalf("Errors = %v", report.Errors)
	}
}

// With an enabled timeline replication job on the rule, the previous newest
// snapshot is the next send's incremental parent and survives keep.hourly=1.
func TestRunKeepsReplicationParent(t *testing.T) {
	for _, withJob := range []bool{true, false} {
		t.Run(fmt.Sprintf("job=%v", withJob), func(t *testing.T) {
			useTempAlerts(t)
			old := ReplicationPath
			ReplicationPath = filepath.Join(t.TempDir(), "btrfs-replication.json")
			t.Cleanup(func() { ReplicationPath = old })
			if withJob {
				job := BtrfsReplicationJob{ID: "job", UUID: rootUUID, Path: "@", Enabled: true, After: AfterTimeline}
				if _, err := UpdateJobs(context.Background(), func(jobs *[]BtrfsReplicationJob) error {
					*jobs = append(*jobs, job)
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			top := t.TempDir()
			stubMounts(t, []Mount{{Mountpoint: top, UUID: rootUUID, SubvolID: 5}})
			now := snapshotTime
			snap := func(ago time.Duration) string { return SnapshotPath("@", now.Add(-ago), KindTimeline) }
			stubRunnerBtrfs(t, top, []subvolumeRow{
				snapshotSubvolume(1, snap(3*time.Hour)),
				snapshotSubvolume(2, snap(2*time.Hour)),
			})

			rules := []BtrfsSnapshotRule{{UUID: rootUUID, Path: "@", Enabled: true, Every: EveryHourly, Keep: BtrfsKeep{Hourly: 1}}}
			report, err := Run(context.Background(), rules, now, KindTimeline)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			var pruned []string
			for _, s := range report.Pruned {
				pruned = append(pruned, s.Path)
			}
			want := []string{snap(3 * time.Hour), snap(2 * time.Hour)}
			if withJob {
				want = want[:1]
			}
			if !reflect.DeepEqual(pruned, want) {
				t.Fatalf("pruned = %v, want %v", pruned, want)
			}
		})
	}
}

func TestSnapshotAlertKey(t *testing.T) {
	long := strings.Repeat("a", 100)
	for path, want := range map[string]string{
		"@":           "snapshot-3f1c0a52--",
		"data/Photos": "snapshot-3f1c0a52-data-photos",
		"x y.z_1":     "snapshot-3f1c0a52-x-y.z_1",
		long:          ("snapshot-3f1c0a52-" + long)[:64],
	} {
		if got := snapshotAlertKey(rootUUID, path); got != want {
			t.Errorf("snapshotAlertKey(%q) = %q, want %q", path, got, want)
		}
	}
}
