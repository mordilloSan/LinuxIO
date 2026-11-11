package dockerupdate

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
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
	if a.ID != "docker-update/nextcloud_app" || a.Severity != alerts.SeverityError || a.Link != dockerUpdateAlertLink {
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
			if err != nil || len(doc.Alerts) != 1 || doc.Alerts[0].ID != "docker-update/web" || doc.Alerts[0].Severity != alerts.SeverityError {
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
		err := runScheduledPass(t.Context(), DockerContainerAutoUpdateOptions{
			Mode:           "update",
			ContainerNames: []string{"web"},
		}, reportingDocker(report, false), scheduledPassOperations{
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
			err := runScheduledPass(t.Context(), DockerContainerAutoUpdateOptions{
				Mode:           "update",
				ContainerNames: []string{"web"},
			}, reportingDocker(alerts.ReportEveryRun, tc.available), scheduledPassOperations{
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
	names := []string{"run", "updated-run", "pass"}
	for _, name := range names {
		for _, key := range []string{dockerUpdateAlertKey(name), "updated-" + dockerUpdateAlertKey(name), "available-" + dockerUpdateAlertKey(name)} {
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
		if a := byID["docker-update/"+name]; a.Severity != alerts.SeverityError || a.Title != "Docker update failed: "+name {
			t.Fatalf("failure of %q = %+v, want it kept beside the notice", name, a)
		}
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
