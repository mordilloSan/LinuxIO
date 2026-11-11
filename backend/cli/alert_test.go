package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

func useTempAlertStore(t *testing.T) {
	t.Helper()
	old := alerts.Path
	alerts.Path = filepath.Join(t.TempDir(), "alerts.json")
	t.Cleanup(func() { alerts.Path = old })
}

func TestParseAptUnattendedLogTakesLastRun(t *testing.T) {
	log := strings.Join([]string{
		"2026-10-07 06:00:00,000 INFO Starting unattended upgrades script",
		"2026-10-07 06:00:05,000 INFO Packages that were upgraded: old1 old2",
		"2026-10-08 06:00:00,000 INFO Starting unattended upgrades script",
		"2026-10-08 06:00:01,000 INFO Packages that will be upgraded: curl libssl3",
		"2026-10-08 06:00:09,000 ERROR Installing the upgrades failed!",
		"2026-10-08 06:00:09,000 INFO Packages that were upgraded: curl",
	}, "\n")
	run := parseAptUnattendedLog(strings.NewReader(log))
	if strings.Join(run.Upgraded, ",") != "curl" {
		t.Fatalf("upgraded = %v", run.Upgraded)
	}
	if len(run.Errors) != 1 || !strings.Contains(run.Errors[0], "Installing the upgrades failed") {
		t.Fatalf("errors = %v", run.Errors)
	}
}

func TestParseAptUnattendedLogNothingToDo(t *testing.T) {
	log := "2026-10-08 06:00:00,000 INFO Starting unattended upgrades script\n" +
		"2026-10-08 06:00:01,000 INFO No packages found that can be upgraded unattended and no pending auto-removals\n"
	run := parseAptUnattendedLog(strings.NewReader(log))
	if len(run.Upgraded) != 0 || len(run.Errors) != 0 {
		t.Fatalf("unexpected run %+v", run)
	}
}

func TestAutoUpdateAlertPolicyMatrix(t *testing.T) {
	cases := []struct {
		policy, result string
		upgraded       []string
		errs           []string
		reboot         bool
		want           bool
		severity       string
	}{
		{"on_failure", "success", nil, nil, false, false, ""},
		{"on_failure", "success", []string{"a"}, nil, false, false, ""},
		{"on_failure", "exit-code", nil, nil, false, true, alerts.SeverityError},
		{"on_failure", "success", nil, []string{"ERROR x"}, false, true, alerts.SeverityError},
		{"on_change", "success", nil, nil, false, false, ""},
		{"on_change", "success", []string{"a"}, nil, false, true, alerts.SeverityInfo},
		{"on_change", "success", []string{"a"}, nil, true, true, alerts.SeverityWarning},
		{"always", "success", nil, nil, false, true, alerts.SeverityInfo},
		{"always", "success", nil, nil, true, true, alerts.SeverityWarning},
	}
	for i, c := range cases {
		got, ok := autoUpdateAlert(c.policy, c.result, autoUpdateRun{Upgraded: c.upgraded, Errors: c.errs}, c.reboot, "inv-1")
		if ok != c.want {
			t.Fatalf("case %d: notify = %v, want %v", i, ok, c.want)
		}
		if ok && got.Severity != c.severity {
			t.Fatalf("case %d: severity = %s, want %s", i, got.Severity, c.severity)
		}
		if ok && (got.Source != "auto-update" || got.Key != "run" || got.OccurrenceID != "inv-1" || got.Link != "/updates") {
			t.Fatalf("case %d: identity %+v", i, got)
		}
	}
}

// The message is cut to alerts.MaxMessageLen by alerts.Raise, not here.
func TestAutoUpdateAlertLongPackageList(t *testing.T) {
	pkgs := make([]string, 0, 1000)
	for range 1000 {
		pkgs = append(pkgs, "package-with-a-long-name-"+strings.Repeat("x", 20))
	}
	got, ok := autoUpdateAlert("on_change", "success", autoUpdateRun{Upgraded: pkgs}, false, "inv")
	if !ok {
		t.Fatal("expected an alert")
	}
	if !strings.Contains(got.Title, "1000 packages") || !strings.HasSuffix(got.Message, pkgs[len(pkgs)-1]) {
		t.Fatalf("title = %q message tail = %q", got.Title, got.Message[len(got.Message)-40:])
	}
}

func TestRunAlertRaiseWritesStore(t *testing.T) {
	useTempAlertStore(t)
	code := runAlert(context.Background(), []string{"raise", "--source", "test", "--key", "k", "--title", "Hello", "--severity", "warning", "--link", "/updates"})
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	doc, err := alerts.Load()
	if err != nil || len(doc.Alerts) != 1 || doc.Alerts[0].Severity != "warning" {
		t.Fatalf("store = %+v err = %v", doc, err)
	}
	if code := runAlert(context.Background(), []string{"resolve", "--source", "test", "--key", "k"}); code != 0 {
		t.Fatalf("resolve exit %d", code)
	}
	doc, _ = alerts.Load()
	if len(doc.Alerts) != 0 {
		t.Fatalf("resolve left %+v", doc.Alerts)
	}
}

func TestRunAlertUsageErrors(t *testing.T) {
	useTempAlertStore(t)
	if code := runAlert(context.Background(), nil); code != 1 {
		t.Fatalf("no args exit %d", code)
	}
	if code := runAlert(context.Background(), []string{"raise", "--source", "test"}); code != 1 {
		t.Fatalf("missing flags exit %d", code)
	}
	if code := runAlert(context.Background(), []string{"auto-update", "--provider", "apt", "--policy", "sometimes"}); code != 1 {
		t.Fatalf("bad policy exit %d", code)
	}
}

func TestRunAlertAutoUpdateAptEndToEnd(t *testing.T) {
	useTempAlertStore(t)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "unattended-upgrades.log")
	_ = os.WriteFile(logPath, []byte("2026-10-08 06:00:00,000 INFO Starting unattended upgrades script\n2026-10-08 06:00:09,000 INFO Packages that were upgraded: curl\n"), 0o644)
	oldLog, oldReboot, oldEnv := aptUnattendedLogPath, rebootRequiredPath, lookupEnv
	aptUnattendedLogPath = logPath
	rebootRequiredPath = filepath.Join(dir, "reboot-required")
	lookupEnv = func(k string) string {
		return map[string]string{"SERVICE_RESULT": "success", "INVOCATION_ID": "abc"}[k]
	}
	t.Cleanup(func() { aptUnattendedLogPath, rebootRequiredPath, lookupEnv = oldLog, oldReboot, oldEnv })

	if code := runAlert(context.Background(), []string{"auto-update", "--provider", "apt", "--policy", "on_change"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	doc, _ := alerts.Load()
	if len(doc.Alerts) != 1 || doc.Alerts[0].OccurrenceID != "abc" || doc.Alerts[0].Severity != alerts.SeverityInfo {
		t.Fatalf("store = %+v", doc.Alerts)
	}
	// Same invocation again is silent.
	_ = runAlert(context.Background(), []string{"auto-update", "--provider", "apt", "--policy", "on_change"})
	doc, _ = alerts.Load()
	if doc.Alerts[0].OccurrenceCount != 1 {
		t.Fatalf("count = %d", doc.Alerts[0].OccurrenceCount)
	}
}

func TestAlwaysWithoutPackageListSaysItRan(t *testing.T) {
	dir := t.TempDir()
	oldLog, oldReboot, oldEnv := aptUnattendedLogPath, rebootRequiredPath, lookupEnv
	aptUnattendedLogPath = filepath.Join(dir, "unattended-upgrades.log")
	rebootRequiredPath = filepath.Join(dir, "reboot-required")
	lookupEnv = func(k string) string {
		return map[string]string{"SERVICE_RESULT": "success", "INVOCATION_ID": "inv-1"}[k]
	}
	t.Cleanup(func() { aptUnattendedLogPath, rebootRequiredPath, lookupEnv = oldLog, oldReboot, oldEnv })
	if err := os.WriteFile(aptUnattendedLogPath, []byte("2026-10-08 06:00:00,000 INFO Starting unattended upgrades script\n2026-10-08 06:00:01,000 INFO No packages found that can be upgraded unattended and no pending auto-removals\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for provider, want := range map[string]string{
		"dnf":  "Automatic updates ran",
		"mint": "Automatic updates ran",
		// unattended-upgrades' log says what a run installed.
		"apt": "Automatic updates found nothing to install",
	} {
		t.Run(provider, func(t *testing.T) {
			useTempAlertStore(t)
			if code := runAlert(t.Context(), []string{"auto-update", "--provider", provider, "--policy", "always"}); code != 0 {
				t.Fatalf("exit %d", code)
			}
			doc, err := alerts.Load()
			if err != nil || len(doc.Alerts) != 1 {
				t.Fatalf("store = %+v err = %v", doc.Alerts, err)
			}
			if a := doc.Alerts[0]; a.ID != "auto-update/run" || a.Title != want || a.Severity != alerts.SeverityInfo {
				t.Fatalf("alert = %+v, want info %q", a, want)
			}
		})
	}
}

func TestRunAlertPendingUpdatesRaisesThenResolves(t *testing.T) {
	useTempAlertStore(t)
	output := "NOTE: This is only a simulation!\nInst zlib1g [1:1.3] (1:1.3.1 Ubuntu:resolute [amd64])\nConf zlib1g (1:1.3.1 Ubuntu:resolute [amd64])\nInst curl [8.5] (8.6 Ubuntu:resolute-security [amd64])\n"
	old := pendingExecCommand
	pendingExecCommand = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "printf", "%s", output)
	}
	t.Cleanup(func() { pendingExecCommand = old })

	if code := runAlert(context.Background(), []string{"pending-updates", "--provider", "apt"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	doc, _ := alerts.Load()
	if len(doc.Alerts) != 1 || doc.Alerts[0].ID != "auto-update/pending" || doc.Alerts[0].Title != "2 system updates available" || doc.Alerts[0].Message != "curl zlib1g" {
		t.Fatalf("store = %+v", doc.Alerts)
	}
	first := doc.Alerts[0].OccurrenceID
	// The same set again is not a new occurrence.
	_ = runAlert(context.Background(), []string{"pending-updates", "--provider", "apt"})
	doc, _ = alerts.Load()
	if doc.Alerts[0].OccurrenceCount != 1 || doc.Alerts[0].OccurrenceID != first {
		t.Fatalf("repeat = %+v", doc.Alerts[0])
	}

	output = "NOTE: This is only a simulation!\n"
	_ = runAlert(context.Background(), []string{"pending-updates", "--provider", "apt"})
	doc, _ = alerts.Load()
	if len(doc.Alerts) != 0 {
		t.Fatalf("nothing pending kept %+v", doc.Alerts)
	}
}

func TestRunAlertPendingUpdatesReportsAptFailure(t *testing.T) {
	useTempAlertStore(t)
	old := pendingExecCommand
	pendingExecCommand = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "echo 'E: Could not open lock' >&2; exit 100")
	}
	t.Cleanup(func() { pendingExecCommand = old })
	if code := runAlert(context.Background(), []string{"pending-updates", "--provider", "apt"}); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if code := runAlert(context.Background(), []string{"pending-updates", "--provider", "dnf"}); code != 1 {
		t.Fatalf("dnf exit %d, want 1", code)
	}
}
