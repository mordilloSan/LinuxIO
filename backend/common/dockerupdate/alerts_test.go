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
