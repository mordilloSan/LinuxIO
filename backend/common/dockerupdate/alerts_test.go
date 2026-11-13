package dockerupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

func useTempAlertStore(t *testing.T) {
	t.Helper()
	old := alerts.Path
	alerts.Path = filepath.Join(t.TempDir(), "alerts.json")
	t.Cleanup(func() { alerts.Path = old })
}

func TestDockerUpdateAlertKeyFallsBackToUnknown(t *testing.T) {
	if got := dockerUpdateAlertKey(""); got != "unknown" {
		t.Fatalf("key(\"\") = %q, want unknown", got)
	}
	if got := dockerUpdateAlertKey("plain"); got != "plain" {
		t.Fatalf("key(plain) = %q", got)
	}
}

func TestRaiseAndResolveDockerUpdateAlert(t *testing.T) {
	useTempAlertStore(t)
	ctx := context.Background()
	raiseDockerUpdateAlert(ctx, "/Nextcloud_App", errors.New("verify replacement: unhealthy"))
	doc, err := alerts.Load()
	if err != nil || len(doc.Alerts) != 1 {
		t.Fatalf("store = %+v err = %v", doc, err)
	}
	a := doc.Alerts[0]
	if a.ID != "docker-update/failed-nextcloud_app" || a.Severity != alerts.SeverityError || a.Link != dockerUpdateAlertLink {
		t.Fatalf("alert = %+v", a)
	}
	// Same error again is silent; a different error is a new occurrence.
	raiseDockerUpdateAlert(ctx, "/Nextcloud_App", errors.New("verify replacement: unhealthy"))
	raiseDockerUpdateAlert(ctx, "/Nextcloud_App", errors.New("pull failed"))
	doc, _ = alerts.Load()
	if doc.Alerts[0].OccurrenceCount != 2 {
		t.Fatalf("count = %d", doc.Alerts[0].OccurrenceCount)
	}
	resolveDockerUpdateAlert(ctx, "Nextcloud_App")
	doc, _ = alerts.Load()
	if len(doc.Alerts) != 0 {
		t.Fatalf("resolve left %+v", doc.Alerts)
	}
}

func TestDockerUpdateAlertFailureDoesNotPanic(t *testing.T) {
	old := alerts.Path
	alerts.Path = t.TempDir() // a directory: reads and writes fail
	t.Cleanup(func() { alerts.Path = old })
	raiseDockerUpdateAlert(context.Background(), "x", errors.New("boom"))
	resolveDockerUpdateAlert(context.Background(), "x")
}

// saveNotify stores n in place of the stored settings.
func saveNotify(t *testing.T, n alerts.NotificationSettings) {
	t.Helper()
	if _, err := alerts.UpdateNotifications(t.Context(), func(stored *alerts.NotificationSettings) error {
		*stored = n
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func alertIDs(t *testing.T) []string {
	t.Helper()
	doc, err := alerts.Load()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(doc.Alerts))
	for _, a := range doc.Alerts {
		ids = append(ids, a.ID)
	}
	return ids
}

// reportingDocker is the settings of a pass whose Docker updates report
// report, with the "updates are waiting" alerts on when available is.
func reportingDocker(report string, available bool) alerts.NotificationSettings {
	return alerts.NotificationSettings{Report: map[string]string{alerts.KindDockerUpdates: report}, DockerUpdatesAvailable: available}
}

func TestDockerFailureIsAlwaysReported(t *testing.T) {
	for name, n := range map[string]alerts.NotificationSettings{
		"stored never": {DockerUpdates: alerts.DockerUpdateNever},
		"failures":     {Report: map[string]string{alerts.KindDockerUpdates: alerts.ReportFailures}},
		"changes":      {Report: map[string]string{alerts.KindDockerUpdates: alerts.ReportChanges}},
		"every run":    {Report: map[string]string{alerts.KindDockerUpdates: alerts.ReportEveryRun}},
	} {
		t.Run(name, func(t *testing.T) {
			useTempAlertStore(t)
			saveNotify(t, n)
			raiseDockerUpdateAlert(t.Context(), "/web", errors.New("pull failed"))
			doc, err := alerts.Load()
			if err != nil || len(doc.Alerts) != 1 || doc.Alerts[0].ID != "docker-update/failed-web" || doc.Alerts[0].Severity != alerts.SeverityError {
				t.Fatalf("alerts = %+v err = %v, want the docker-update/web error", doc.Alerts, err)
			}
		})
	}
}

func TestDockerUpdatedFollowsTheReport(t *testing.T) {
	for report, want := range map[string]bool{
		alerts.ReportFailures: false,
		alerts.ReportChanges:  true,
		alerts.ReportEveryRun: true,
	} {
		t.Run(report, func(t *testing.T) {
			useTempAlertStore(t)
			raiseDockerUpdatedAlert(t.Context(), report, "/web", "sha256:old", "sha256:new")
			doc, err := alerts.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !want {
				if len(doc.Alerts) != 0 {
					t.Fatalf("alerts = %+v, want none", doc.Alerts)
				}
				return
			}
			if len(doc.Alerts) != 1 || doc.Alerts[0].ID != "docker-update/updated-web" ||
				doc.Alerts[0].Severity != alerts.SeverityInfo || doc.Alerts[0].Title != "Docker container updated: web" {
				t.Fatalf("alerts = %+v", doc.Alerts)
			}
			// The record survives the resolve a successful update performs.
			resolveDockerUpdateAlert(t.Context(), "/web")
			if ids := alertIDs(t); len(ids) != 1 {
				t.Fatalf("alerts after resolve = %v", ids)
			}
		})
	}
}

// storedPassNotice is the pass notice in the store, or the zero Alert.
func storedPassNotice(t *testing.T) alerts.Alert {
	t.Helper()
	doc, err := alerts.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range doc.Alerts {
		if a.ID == "docker-update/linuxio-update-pass" {
			return a
		}
	}
	return alerts.Alert{}
}

func TestDockerEveryRunReportsAQuietPass(t *testing.T) {
	useTempAlertStore(t)
	pass := func(report string, uncheckable, updated int, updateErr error) {
		t.Helper()
		err := runScheduledPass(t.Context(), DockerContainerAutoUpdateOptions{Mode: "update", ContainerNames: []string{"web"}}, reportingDocker(report, false), scheduledPassOperations{
			list: func(context.Context) ([]container.Summary, error) {
				return []container.Summary{{ID: "web-id", Names: []string{"/web"}, State: container.StateRunning}}, nil
			},
			check: func(context.Context, []container.Summary) (DockerUpdateCheckResult, error) {
				return DockerUpdateCheckResult{Checked: 1, Errors: uncheckable}, nil
			},
			update: func(context.Context, []container.Summary, []container.Summary, DockerContainerAutoUpdateOptions, func(string)) (int, error) {
				return updated, updateErr
			},
		}, nil)
		if !errors.Is(err, updateErr) {
			t.Fatalf("runScheduledPass = %v, want %v", err, updateErr)
		}
	}

	pass(alerts.ReportFailures, 0, 0, nil)
	pass(alerts.ReportChanges, 0, 0, nil)
	if a := storedPassNotice(t); a.ID != "" {
		t.Fatalf("quiet pass reported without every_run: %+v", a)
	}

	pass(alerts.ReportEveryRun, 0, 0, nil)
	a := storedPassNotice(t)
	if a.Severity != alerts.SeverityInfo || a.Title != "Docker updates: nothing to update" ||
		a.Link != dockerUpdateAlertLink || a.OccurrenceCount != 1 {
		t.Fatalf("quiet pass notice = %+v", a)
	}
	// The next quiet pass is a new occurrence of the one notice, not a second record.
	pass(alerts.ReportEveryRun, 0, 0, nil)
	if ids := alertIDs(t); len(ids) != 1 || storedPassNotice(t).OccurrenceCount != 2 {
		t.Fatalf("second quiet pass: alerts %v, notice %+v", ids, storedPassNotice(t))
	}
	// A pass that replaced a container raised its own notice; one that failed
	// raised its alerts; one whose check could not check a container cannot
	// say there was nothing to update.
	pass(alerts.ReportEveryRun, 0, 1, nil)
	pass(alerts.ReportEveryRun, 0, 0, errors.New("pull failed"))
	pass(alerts.ReportEveryRun, 1, 0, nil)
	if got := storedPassNotice(t).OccurrenceCount; got != 2 {
		t.Fatalf("notice occurrences = %d, want 2", got)
	}
}

func TestDockerEveryRunReportsACheckOnlyPass(t *testing.T) {
	tests := []struct {
		name        string
		report      string
		available   bool // the "updates are waiting" alerts are on
		waiting     int
		uncheckable int // containers the check could not check
		checkErr    error
		want        string // the pass notice's title; "" for none
	}{
		{name: "nothing found", report: alerts.ReportEveryRun, available: true, want: "Docker updates: nothing to update"},
		{name: "one waiting, no waiting alerts", report: alerts.ReportEveryRun, waiting: 1, want: "Docker updates: 1 waiting"},
		{name: "two waiting, no waiting alerts", report: alerts.ReportEveryRun, waiting: 2, want: "Docker updates: 2 waiting"},
		{name: "the waiting alerts report it", report: alerts.ReportEveryRun, available: true, waiting: 2},
		{name: "failed check", report: alerts.ReportEveryRun, checkErr: errors.New("registry unreachable")},
		{name: "a container could not be checked", report: alerts.ReportEveryRun, uncheckable: 1},
		{name: "one waiting, one could not be checked", report: alerts.ReportEveryRun, waiting: 1, uncheckable: 1},
		{name: "changes", report: alerts.ReportChanges},
		{name: "failures", report: alerts.ReportFailures, waiting: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			useTempAlertStore(t)
			err := runScheduledPass(t.Context(), DockerContainerAutoUpdateOptions{Mode: "check_only"}, reportingDocker(tc.report, tc.available), scheduledPassOperations{
				list: func(context.Context) ([]container.Summary, error) {
					return []container.Summary{{ID: "web-id", Names: []string{"/web"}, State: container.StateRunning}}, nil
				},
				check: func(context.Context, []container.Summary) (DockerUpdateCheckResult, error) {
					return DockerUpdateCheckResult{Checked: 2, Updates: tc.waiting, Errors: tc.uncheckable}, tc.checkErr
				},
				update: func(context.Context, []container.Summary, []container.Summary, DockerContainerAutoUpdateOptions, func(string)) (int, error) {
					t.Fatal("a check-only pass updated")
					return 0, nil
				},
			}, nil)
			if !errors.Is(err, tc.checkErr) {
				t.Fatalf("runScheduledPass = %v, want %v", err, tc.checkErr)
			}
			a := storedPassNotice(t)
			if a.Title != tc.want {
				t.Fatalf("pass notice = %+v, want title %q", a, tc.want)
			}
			if tc.want != "" && (a.Severity != alerts.SeverityInfo || a.Link != dockerUpdateAlertLink || a.OccurrenceCount != 1) {
				t.Fatalf("pass notice = %+v", a)
			}
		})
	}
}

// An updating pass that replaced nothing reports like a check-only pass:
// updates can wait for containers it is not set to update.
func TestDockerEveryRunReportsAnUpdatePassThatReplacedNothing(t *testing.T) {
	tests := []struct {
		name      string
		available bool // the "updates are waiting" alerts are on
		waiting   int
		updated   int
		want      string // the pass notice's title; "" for none
	}{
		{name: "nothing waits", want: "Docker updates: nothing to update"},
		{name: "two wait, no waiting alerts", waiting: 2, want: "Docker updates: 2 waiting"},
		{name: "the waiting alerts report them", available: true, waiting: 2},
		{name: "a container was replaced", waiting: 2, updated: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			useTempAlertStore(t)
			err := runScheduledPass(t.Context(), DockerContainerAutoUpdateOptions{Mode: "update", ContainerNames: []string{"web"}}, reportingDocker(alerts.ReportEveryRun, tc.available), scheduledPassOperations{
				list: func(context.Context) ([]container.Summary, error) {
					return []container.Summary{
						{ID: "web-id", Names: []string{"/web"}, State: container.StateRunning},
						{ID: "db-id", Names: []string{"/db"}, State: container.StateRunning},
					}, nil
				},
				check: func(context.Context, []container.Summary) (DockerUpdateCheckResult, error) {
					return DockerUpdateCheckResult{Checked: 2, Updates: tc.waiting}, nil
				},
				update: func(context.Context, []container.Summary, []container.Summary, DockerContainerAutoUpdateOptions, func(string)) (int, error) {
					return tc.updated, nil
				},
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if a := storedPassNotice(t); a.Title != tc.want {
				t.Fatalf("pass notice = %+v, want title %q", a, tc.want)
			}
		})
	}
}

// The pass notice shares its source with every container's alerts: no
// container's failure, updated or available record may land on its key.
func TestDockerPassNoticeKeyDiffersFromContainerKeys(t *testing.T) {
	names := []string{"run", "updated-run", "pass", dockerUpdatePassAlertKey}
	for _, name := range names {
		for _, key := range []string{dockerUpdateFailedKeyPrefix + dockerUpdateAlertKey(name), dockerUpdateUpdatedKeyPrefix + dockerUpdateAlertKey(name), dockerUpdateAvailableKeyPrefix + dockerUpdateAlertKey(name)} {
			if key == dockerUpdatePassAlertKey {
				t.Fatalf("container %q has the pass notice's key %q", name, key)
			}
		}
	}

	// A quiet pass after failed ones leaves every failure as it was.
	useTempAlertStore(t)
	for _, name := range names {
		raiseDockerUpdateAlert(t.Context(), "/"+name, errors.New("pull failed"))
	}
	raiseDockerPassAlert(t.Context(), time.Now(), 0)
	doc, err := alerts.Load()
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]alerts.Alert, len(doc.Alerts))
	for _, a := range doc.Alerts {
		byID[a.ID] = a
	}
	if len(byID) != len(names)+1 || byID["docker-update/"+dockerUpdatePassAlertKey].Severity != alerts.SeverityInfo {
		t.Fatalf("alerts = %v, want the three failures and the notice", alertIDs(t))
	}
	for _, name := range names {
		if a := byID["docker-update/failed-"+name]; a.Severity != alerts.SeverityError || a.Title != "Docker update failed: "+name {
			t.Fatalf("failure of %q = %+v, want it kept beside the notice", name, a)
		}
	}
}

// Containers named like another kind's key stay separate: "updated-x" failing
// is not the "updated" notice of "x".
func TestContainerKeysNeverCollide(t *testing.T) {
	names := []string{"x", "updated-x", "available-x", "failed-x", "updated-updated-x", dockerUpdatePassAlertKey}
	seen := map[string]string{}
	for _, name := range names {
		for _, key := range []string{dockerUpdateFailedKeyPrefix + name, dockerUpdateUpdatedKeyPrefix + name, dockerUpdateAvailableKeyPrefix + name} {
			if other, ok := seen[key]; ok {
				t.Errorf("key %q is both %s and %q", key, other, name)
			}
			seen[key] = name
		}
	}
	if _, ok := seen[dockerUpdatePassAlertKey]; ok {
		t.Errorf("a container has the pass notice's key")
	}

	useTempAlertStore(t)
	raiseDockerUpdatedAlert(t.Context(), alerts.ReportChanges, "/x", "sha256:old", "sha256:new")
	raiseDockerUpdateAlert(t.Context(), "/updated-x", errors.New("pull failed"))
	if ids := alertIDs(t); len(ids) != 2 {
		t.Fatalf("alerts = %v, want the notice of x and the failure of updated-x", ids)
	}
}

func TestSyncAvailableAlertsFollowsSettingAndState(t *testing.T) {
	useTempAlertStore(t)
	ctx := context.Background()
	statuses := []ImageUpdateStatus{
		{ContainerName: "web", CheckState: DockerUpdateCheckStateAvailable, ImageRef: "nginx:latest", RemoteDigest: "sha256:r1"},
		{ContainerName: "db", CheckState: DockerUpdateCheckStateCurrent},
	}
	syncAvailableAlerts(ctx, statuses, false)
	if ids := alertIDs(t); len(ids) != 0 {
		t.Fatalf("setting off raised %v", ids)
	}
	syncAvailableAlerts(ctx, statuses, true)
	if ids := alertIDs(t); len(ids) != 1 || ids[0] != "docker-update/available-web" {
		t.Fatalf("alerts = %v", ids)
	}
	statuses[0].CheckState = DockerUpdateCheckStateCurrent
	syncAvailableAlerts(ctx, statuses, true)
	if ids := alertIDs(t); len(ids) != 0 {
		t.Fatalf("current container kept %v", ids)
	}
	// A replaced container clears its pending record too.
	statuses[0].CheckState = DockerUpdateCheckStateAvailable
	syncAvailableAlerts(ctx, statuses, true)
	resolveDockerUpdateAlert(ctx, "/web")
	if ids := alertIDs(t); len(ids) != 0 {
		t.Fatalf("resolve kept %v", ids)
	}
}

// A container whose registry could not be asked is not known to be current:
// the failed check is a warning until a check of it succeeds, raised once
// however many passes it lasts.
func TestSyncCheckAlertsFollowsTheCheckError(t *testing.T) {
	useTempAlertStore(t)
	ctx := context.Background()
	statuses := []ImageUpdateStatus{
		{ContainerName: "/web", CheckState: DockerUpdateCheckStateError, ImageRef: "nginx:latest", Err: "registry unreachable"},
		{ContainerName: "db", CheckState: DockerUpdateCheckStateCurrent},
		{ContainerName: "local", CheckState: DockerUpdateCheckStateUncheckable},
	}
	// The failure alert of a container with the same name is another record.
	raiseDockerUpdateAlert(ctx, "/web", errors.New("pull failed"))
	syncCheckAlerts(ctx, statuses)
	doc, err := alerts.Load()
	if err != nil || len(doc.Alerts) != 2 {
		t.Fatalf("alerts = %+v, %v; want the update failure and the failed check", doc.Alerts, err)
	}
	check := func(doc alerts.Document) alerts.Alert {
		for _, a := range doc.Alerts {
			if a.ID == "docker-update-check/web" {
				return a
			}
		}
		t.Fatalf("no failed-check warning in %+v", doc.Alerts)
		return alerts.Alert{}
	}
	got := check(doc)
	if got.Severity != alerts.SeverityWarning || got.Title != "Docker update check failed: web" ||
		got.Message != "The image nginx:latest could not be checked for updates. The containers page shows the reason." || got.Link != dockerUpdateAlertLink {
		t.Fatalf("alert = %+v", got)
	}

	// The next pass fails for another reason: the same occurrence.
	statuses[0].Err = "unauthorized"
	syncCheckAlerts(ctx, statuses)
	if doc, _ = alerts.Load(); len(doc.Alerts) != 2 || check(doc).OccurrenceCount != 1 {
		t.Fatalf("second failing pass: alerts = %+v, want one occurrence of the check", doc.Alerts)
	}

	statuses[0].CheckState, statuses[0].Err = DockerUpdateCheckStateCurrent, ""
	syncCheckAlerts(ctx, statuses)
	if ids := alertIDs(t); len(ids) != 1 || ids[0] != "docker-update/failed-web" {
		t.Fatalf("after a successful check: %v, want only the update failure", ids)
	}

	// A container that is gone, or no longer checked, loses its warning too.
	statuses[0].CheckState, statuses[0].Err = DockerUpdateCheckStateError, "registry unreachable"
	syncCheckAlerts(ctx, statuses)
	syncCheckAlerts(ctx, statuses[1:])
	if ids := alertIDs(t); len(ids) != 1 || ids[0] != "docker-update/failed-web" {
		t.Fatalf("after the container left the check: %v", ids)
	}

	// A replaced container clears its failed check too.
	syncCheckAlerts(ctx, statuses)
	resolveDockerUpdateAlert(ctx, "/web")
	if ids := alertIDs(t); len(ids) != 0 {
		t.Fatalf("resolve kept %v", ids)
	}
}

// A failure with its container's alert is marked reported, so the worker's
// exit status tells the end hook that no run alert is needed.
func TestScheduledUpdateErrorIsReported(t *testing.T) {
	useTempAlertStore(t)
	old := updateStatusPath
	updateStatusPath = filepath.Join(t.TempDir(), "status.json")
	t.Cleanup(func() { updateStatusPath = old })

	cause := errors.New("pull failed")
	err := scheduledUpdateError(t.Context(), container.InspectResponse{ID: "abc", Name: "/web"}, cause)
	if !scheduledrun.AllReported(err) || !errors.Is(err, cause) {
		t.Fatalf("err = %v, want the cause, marked reported", err)
	}
	if ids := alertIDs(t); len(ids) != 1 || ids[0] != "docker-update/failed-web" {
		t.Fatalf("alerts = %v, want the container's", ids)
	}
	if scheduledrun.AllReported(errors.Join(err, errors.New("a scheduled container is gone"))) {
		t.Fatal("a pass with an unreported failure counts as reported")
	}
}

// A failed Compose update is reported by each of its containers' alerts; a
// status file that could not be written beside it is not, so the pass still
// needs its run alert.
func TestComposeGroupErrorIsReportedExceptForTheStatusFile(t *testing.T) {
	useTempAlertStore(t)
	old := updateStatusPath
	t.Cleanup(func() { updateStatusPath = old })
	group := &scheduledComposeTarget{before: []container.InspectResponse{{ID: "a", Name: "/web"}, {ID: "b", Name: "/db"}}}
	cause := errors.New("compose up failed")

	updateStatusPath = filepath.Join(t.TempDir(), "status.json")
	state := &scheduledUpdateState{ctx: t.Context()}
	state.recordComposeGroupError(group, cause)
	if err := errors.Join(state.errs...); !scheduledrun.AllReported(err) || !errors.Is(err, cause) {
		t.Fatalf("errs = %v, want the cause, marked reported", state.errs)
	}
	if ids := alertIDs(t); len(ids) != 2 {
		t.Fatalf("alerts = %v, want one per container", ids)
	}

	// The status file's directory is a file: the status cannot be written.
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	updateStatusPath = filepath.Join(blocked, "status.json")
	state = &scheduledUpdateState{ctx: t.Context()}
	state.recordComposeGroupError(group, cause)
	if err := errors.Join(state.errs...); scheduledrun.AllReported(err) || !strings.Contains(err.Error(), "record the failed update of web") {
		t.Fatalf("errs = %v, want the status-file failure kept unmarked", state.errs)
	}
}

// A scheduled pass that finds another update holding the lock did no work.
func TestPassLockHeldByAnotherUpdateIsSkipped(t *testing.T) {
	old := acquireLock
	t.Cleanup(func() { acquireLock = old })
	acquireLock = func(context.Context) (func(), error) { return nil, errUpdateInProgress }
	if _, err := acquirePassLock(t.Context()); scheduledrun.ExitStatus(err) != scheduledrun.ExitSkipped || !errors.Is(err, errUpdateInProgress) {
		t.Fatalf("err = %v, exit status %d; want the lock error marked skipped", err, scheduledrun.ExitStatus(err))
	}
	other := errors.New("lock file cannot be opened")
	acquireLock = func(context.Context) (func(), error) { return nil, other }
	if _, err := acquirePassLock(t.Context()); scheduledrun.ExitStatus(err) != 1 {
		t.Fatalf("err = %v, exit status %d; want an ordinary failure", err, scheduledrun.ExitStatus(err))
	}
}

// A name the store normalises is still matched: the warning of a container
// that keeps failing stays one occurrence instead of being cleared and raised
// again each pass.
func TestSyncCheckAlertsKeepsAWarningWhoseNameIsNormalised(t *testing.T) {
	useTempAlertStore(t)
	statuses := []ImageUpdateStatus{{ContainerName: "/Web_App", CheckState: DockerUpdateCheckStateError, ImageRef: "app:1", Err: "timeout"}}
	syncCheckAlerts(t.Context(), statuses)
	syncCheckAlerts(t.Context(), statuses)
	doc, err := alerts.Load()
	if err != nil || len(doc.Alerts) != 1 || doc.Alerts[0].ID != "docker-update-check/web_app" || doc.Alerts[0].OccurrenceCount != 1 {
		t.Fatalf("alerts = %+v, %v; want the one warning, raised once", doc.Alerts, err)
	}
}
