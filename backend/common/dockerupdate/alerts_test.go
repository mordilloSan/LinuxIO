package dockerupdate

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

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

func saveNotify(t *testing.T, n alerts.NotificationSettings) {
	t.Helper()
	if _, err := alerts.SaveNotifications(n); err != nil {
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

func TestDockerUpdateAlertPolicyNeverSilencesFailures(t *testing.T) {
	useTempAlertStore(t)
	saveNotify(t, alerts.NotificationSettings{DockerUpdates: alerts.DockerUpdateNever})
	raiseDockerUpdateAlert(context.Background(), "/web", errors.New("pull failed"))
	if ids := alertIDs(t); len(ids) != 0 {
		t.Fatalf("alerts = %v, want none", ids)
	}
}

func TestDockerUpdatedAlertOnlyOnChange(t *testing.T) {
	useTempAlertStore(t)
	ctx := context.Background()
	raiseDockerUpdatedAlert(ctx, "/web", "sha256:old", "sha256:new")
	if ids := alertIDs(t); len(ids) != 0 {
		t.Fatalf("default policy raised %v", ids)
	}
	saveNotify(t, alerts.NotificationSettings{DockerUpdates: alerts.DockerUpdateOnChange})
	raiseDockerUpdatedAlert(ctx, "/web", "sha256:old", "sha256:new")
	doc, _ := alerts.Load()
	if len(doc.Alerts) != 1 || doc.Alerts[0].ID != "docker-update/updated-web" || doc.Alerts[0].Severity != alerts.SeverityInfo {
		t.Fatalf("alerts = %+v", doc.Alerts)
	}
	// The record survives the resolve a successful update performs.
	resolveDockerUpdateAlert(ctx, "/web")
	if ids := alertIDs(t); len(ids) != 1 {
		t.Fatalf("alerts after resolve = %v", ids)
	}
}

func TestSyncAvailableAlertsFollowsSettingAndState(t *testing.T) {
	useTempAlertStore(t)
	ctx := context.Background()
	statuses := []ImageUpdateStatus{
		{ContainerName: "web", CheckState: DockerUpdateCheckStateAvailable, ImageRef: "nginx:latest", RemoteDigest: "sha256:r1"},
		{ContainerName: "db", CheckState: DockerUpdateCheckStateCurrent},
	}
	syncAvailableAlerts(ctx, statuses)
	if ids := alertIDs(t); len(ids) != 0 {
		t.Fatalf("setting off raised %v", ids)
	}
	saveNotify(t, alerts.NotificationSettings{DockerUpdatesAvailable: true})
	syncAvailableAlerts(ctx, statuses)
	if ids := alertIDs(t); len(ids) != 1 || ids[0] != "docker-update/available-web" {
		t.Fatalf("alerts = %v", ids)
	}
	statuses[0].CheckState = DockerUpdateCheckStateCurrent
	syncAvailableAlerts(ctx, statuses)
	if ids := alertIDs(t); len(ids) != 0 {
		t.Fatalf("current container kept %v", ids)
	}
	// A replaced container clears its pending record too.
	statuses[0].CheckState = DockerUpdateCheckStateAvailable
	syncAvailableAlerts(ctx, statuses)
	resolveDockerUpdateAlert(ctx, "/web")
	if ids := alertIDs(t); len(ids) != 0 {
		t.Fatalf("resolve kept %v", ids)
	}
}
