package alerts

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

var allKinds = []string{KindDockerUpdates, KindScripts, KindSnapshots, KindResync, KindBrowserJobs}

// writeNotificationsFile stores data as notifications.json, as an older build
// or an admin would have left it.
func writeNotificationsFile(t *testing.T, data string) {
	t.Helper()
	if err := os.WriteFile(notificationsPath(), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// olderBuild is what a build that knows neither report nor levels reads.
type olderBuild struct {
	MinSeverity   string `json:"minSeverity"`
	DockerUpdates string `json:"dockerUpdates"`
}

func readAsOlderBuild(t *testing.T) olderBuild {
	t.Helper()
	data, err := os.ReadFile(notificationsPath())
	if err != nil {
		t.Fatal(err)
	}
	var old olderBuild
	if err := json.Unmarshal(data, &old); err != nil {
		t.Fatalf("an older build cannot read %s: %v", data, err)
	}
	return old
}

func loadNotifications(t *testing.T) NotificationSettings {
	t.Helper()
	n, err := LoadNotifications()
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// saveNotifications stores n in place of the stored settings through
// UpdateNotifications, the write path of the writers besides the settings
// page, and returns what was stored.
func saveNotifications(ctx context.Context, n NotificationSettings) (NotificationSettings, error) {
	return UpdateNotifications(ctx, func(stored *NotificationSettings) error {
		*stored = n
		return nil
	})
}

func TestRaiseWithoutSettingsDeliversNothing(t *testing.T) {
	useTempStore(t)
	if err := Raise(context.Background(), obs("disk")); err != nil {
		t.Fatal(err)
	}
	// Errors are emailed and posted by default, but no channel is configured yet.
	n, err := LoadNotifications()
	if err != nil || n.WebhookURL != "" || n.SMTPHost != "" {
		t.Fatalf("settings = %+v, %v", n, err)
	}
}

func TestSaveKeepsPassword(t *testing.T) {
	useTempStore(t)
	ctx := t.Context()
	base := NotificationSettings{MinSeverity: SeverityError, SMTPHost: "mail.example.com", SMTPUsername: "nas", SMTPPassword: "secret",
		EmailFrom: "nas@example.com", EmailTo: []string{"me@example.com"}}
	saved, err := saveNotifications(ctx, base)
	if err != nil || saved.SMTPPassword != "secret" || saved.SMTPPort != 587 {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	base.SMTPPassword = "" // a save from the UI never carries it back
	if _, err = saveNotifications(ctx, base); err != nil {
		t.Fatal(err)
	}
	if n, _ := LoadNotifications(); n.SMTPPassword != "secret" {
		t.Fatalf("password not kept: %+v", n)
	}
	base.SMTPUsername = ""
	if _, err = saveNotifications(ctx, base); err != nil {
		t.Fatal(err)
	}
	if n, _ := LoadNotifications(); n.SMTPPassword != "" {
		t.Fatalf("password kept without a username: %+v", n)
	}
	info, err := os.Stat(notificationsPath())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, %v", info.Mode(), err)
	}
}

func TestSaveValidates(t *testing.T) {
	useTempStore(t)
	email := NotificationSettings{SMTPHost: "mail.example.com", EmailFrom: "nas@example.com", EmailTo: []string{"me@example.com"}}
	for name, n := range map[string]NotificationSettings{
		"severity":  {MinSeverity: "loud"},
		"webhook":   {WebhookURL: "ftp://x"},
		"no host":   {WebhookURL: "https://"},
		"smtp host": {SMTPHost: "a b", EmailFrom: "x@y.z", EmailTo: []string{"x@y.z"}},
		"port":      {SMTPHost: "h", SMTPPort: 70000, EmailFrom: "x@y.z", EmailTo: []string{"x@y.z"}},
		"from":      {SMTPHost: "h", EmailFrom: "nobody", EmailTo: []string{"x@y.z"}},
		"to":        {SMTPHost: "h", EmailFrom: "x@y.z", EmailTo: []string{"x\r\nBcc: evil@y.z"}},
		"no to":     {SMTPHost: "h", EmailFrom: "x@y.z"},
	} {
		if _, err := saveNotifications(t.Context(), n); err == nil {
			t.Errorf("%s: accepted %+v", name, n)
		}
	}
	if _, err := saveNotifications(t.Context(), email); err != nil {
		t.Fatalf("valid email settings: %v", err)
	}
}

func TestNotificationsDockerUpdatesDefaultAndValidate(t *testing.T) {
	useTempStore(t)
	n, err := LoadNotifications()
	if err != nil || n.DockerUpdates != DockerUpdateOnFailure {
		t.Fatalf("missing file = %+v, %v", n, err)
	}
	if _, err = saveNotifications(t.Context(), NotificationSettings{DockerUpdates: "sometimes"}); err == nil {
		t.Fatal("invalid Docker policy accepted")
	}
	if _, err = saveNotifications(t.Context(), NotificationSettings{DockerUpdatesAvailable: true}); err != nil {
		t.Fatal(err)
	}
	n, err = LoadNotifications()
	if err != nil || n.DockerUpdates != DockerUpdateOnFailure || !n.DockerUpdatesAvailable {
		t.Fatalf("saved = %+v, %v", n, err)
	}
}

func TestLoadMigratesTheLeastSeverity(t *testing.T) {
	for _, least := range []string{"", SeverityError, SeverityWarning, SeverityInfo} {
		t.Run("minSeverity="+least, func(t *testing.T) {
			useTempStore(t)
			writeNotificationsFile(t, `{"minSeverity": "`+least+`", "webhookUrl": "https://hooks.example.com/x"}`)
			n := loadNotifications(t)
			if len(n.Levels) != len(severityRank) {
				t.Fatalf("levels = %+v", n.Levels)
			}
			for severity := range severityRank {
				sent := least != "" && severityRank[severity] >= severityRank[least]
				want := Channels{Browser: true, Email: sent, Webhook: sent}
				if got := n.Levels[severity]; got != want {
					t.Errorf("%s = %+v, want %+v", severity, got, want)
				}
				if got := n.ChannelsFor(severity); got != want {
					t.Errorf("ChannelsFor(%s) = %+v, want %+v", severity, got, want)
				}
			}
			if n.MinSeverity != least {
				t.Errorf("minSeverity = %q, want it as stored", n.MinSeverity)
			}
		})
	}
}

func TestLoadMigratesTheDockerPolicy(t *testing.T) {
	for policy, want := range map[string]string{
		DockerUpdateNever:     ReportFailures,
		DockerUpdateOnFailure: ReportFailures,
		DockerUpdateOnChange:  ReportChanges,
	} {
		t.Run(policy, func(t *testing.T) {
			useTempStore(t)
			writeNotificationsFile(t, `{"dockerUpdates": "`+policy+`"}`)
			n := loadNotifications(t)
			if got := n.ReportFor(KindDockerUpdates); got != want {
				t.Errorf("ReportFor(docker_updates) = %q, want %q", got, want)
			}
			if n.DockerUpdates != policy {
				t.Errorf("dockerUpdates = %q, want it as stored", n.DockerUpdates)
			}
		})
	}
}

func TestStoredReportWinsOverTheOldDockerPolicy(t *testing.T) {
	useTempStore(t)
	writeNotificationsFile(t, `{"report": {"docker_updates": "failures"}, "dockerUpdates": "on_change"}`)
	if got := loadNotifications(t).ReportFor(KindDockerUpdates); got != ReportFailures {
		t.Fatalf("ReportFor(docker_updates) = %q, want the stored report", got)
	}
}

func TestMissingFileHasTheDefaults(t *testing.T) {
	useTempStore(t)
	n := loadNotifications(t)
	for severity := range severityRank {
		sent := severity == SeverityError
		want := Channels{Browser: true, Email: sent, Webhook: sent}
		if got := n.ChannelsFor(severity); got != want {
			t.Errorf("ChannelsFor(%s) = %+v, want %+v", severity, got, want)
		}
		if got := n.Levels[severity]; got != want {
			t.Errorf("levels[%s] = %+v, want %+v", severity, got, want)
		}
	}
	for _, kind := range allKinds {
		want := ReportFailures
		if kind == KindResync {
			want = ReportChanges // a pool that is whole again is reported, as before
		}
		if got := n.ReportFor(kind); got != want {
			t.Errorf("ReportFor(%s) = %q, want %q", kind, got, want)
		}
		if got := n.Report[kind]; got != want {
			t.Errorf("report[%s] = %q, want %q", kind, got, want)
		}
	}
	if got := n.ReportForScript("any"); got != ReportFailures {
		t.Errorf("ReportForScript = %q", got)
	}
}

func TestStoredLevelsWinOverTheOldSetting(t *testing.T) {
	useTempStore(t)
	writeNotificationsFile(t, `{"minSeverity": "error", "levels": {
		"info": {"browser": false, "email": true, "webhook": false},
		"warning": {"browser": true, "email": false, "webhook": true},
		"error": {"browser": true, "email": false, "webhook": false}}}`)
	n := loadNotifications(t)
	want := map[string]Channels{
		SeverityInfo:    {Email: true},
		SeverityWarning: {Browser: true, Webhook: true},
		SeverityError:   {Browser: true},
	}
	for severity, c := range want {
		if got := n.ChannelsFor(severity); got != c {
			t.Errorf("ChannelsFor(%s) = %+v, want %+v", severity, got, c)
		}
	}
}

func TestErrorsAlwaysReachTheBrowser(t *testing.T) {
	useTempStore(t)
	saved, err := saveNotifications(t.Context(), NotificationSettings{Levels: map[string]Channels{
		SeverityError: {Browser: false, Email: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Levels[SeverityError].Browser {
		t.Fatalf("saved = %+v", saved.Levels)
	}
	data, err := os.ReadFile(notificationsPath())
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Levels map[string]Channels `json:"levels"`
	}
	if err := json.Unmarshal(data, &stored); err != nil || !stored.Levels[SeverityError].Browser || !stored.Levels[SeverityError].Email {
		t.Fatalf("stored = %s, %v", data, err)
	}
}

func TestValidateRejectsUnknownReports(t *testing.T) {
	useTempStore(t)
	for name, n := range map[string]NotificationSettings{
		"scripts report no changes":  {Report: map[string]string{KindScripts: ReportChanges}},
		"browser jobs run every day": {Report: map[string]string{KindBrowserJobs: ReportEveryRun}},
		"resync has no long jobs":    {Report: map[string]string{KindResync: ReportLongJobs}},
		"unknown kind":               {Report: map[string]string{"file_indexer": ReportFailures}},
		"unknown report":             {Report: map[string]string{KindSnapshots: "sometimes"}},
		"level that is no severity":  {Levels: map[string]Channels{"critical": {Browser: true}}},
	} {
		if _, err := saveNotifications(t.Context(), n); err == nil {
			t.Errorf("%s: accepted %+v", name, n)
		}
	}
	for kind, offered := range map[string][]string{
		KindDockerUpdates: {ReportFailures, ReportChanges, ReportEveryRun},
		KindScripts:       {ReportFailures, ReportEveryRun},
		KindSnapshots:     {ReportFailures, ReportChanges, ReportEveryRun},
		KindResync:        {ReportFailures, ReportChanges},
		KindBrowserJobs:   {ReportFailures, ReportLongJobs},
	} {
		for _, report := range offered {
			if _, err := saveNotifications(t.Context(), NotificationSettings{Report: map[string]string{kind: report}}); err != nil {
				t.Errorf("%s: %s refused: %v", kind, report, err)
			}
			if !ReportOffered(kind, report) {
				t.Errorf("ReportOffered(%s, %s) = false", kind, report)
			}
		}
	}
	for _, refused := range [][2]string{{KindScripts, ReportChanges}, {KindScripts, ""}, {KindResync, ReportEveryRun}, {"file_indexer", ReportFailures}} {
		if ReportOffered(refused[0], refused[1]) {
			t.Errorf("ReportOffered(%s, %q) = true", refused[0], refused[1])
		}
	}
}

// A value no kind offers, left by a hand edit or by a newer build before a
// downgrade, takes the default instead of failing every later save.
func TestLoadDropsReportsTheKindDoesNotOffer(t *testing.T) {
	useTempStore(t)
	writeNotificationsFile(t, `{"report": {"resync": "every_run"}, "scripts": {"a": "changes"}}`)
	n := loadNotifications(t)
	if got := n.ReportFor(KindResync); got != ReportChanges {
		t.Errorf("ReportFor(resync) = %q, want the default changes", got)
	}
	if report, ok := n.Scripts["a"]; ok {
		t.Errorf("scripts[a] = %q, want it dropped", report)
	}
	if got := n.ReportForScript("a"); got != ReportFailures {
		t.Errorf("ReportForScript(a) = %q, want the scheduled scripts' line", got)
	}
	if _, err := UpdateNotifications(t.Context(), func(n *NotificationSettings) error {
		n.WebhookURL = "https://hooks.example.com/x"
		return nil
	}); err != nil {
		t.Fatalf("save after loading the dropped values: %v", err)
	}
}

func TestReportForScript(t *testing.T) {
	useTempStore(t)
	_, err := saveNotifications(t.Context(), NotificationSettings{
		Report:  map[string]string{KindScripts: ReportEveryRun},
		Scripts: map[string]string{"a": ReportFailures},
	})
	if err != nil {
		t.Fatal(err)
	}
	n := loadNotifications(t)
	if got := n.ReportForScript("a"); got != ReportFailures {
		t.Errorf("override = %q, want failures", got)
	}
	if got := n.ReportForScript("b"); got != ReportEveryRun {
		t.Errorf("without an override = %q, want the scheduled scripts' line", got)
	}
	for _, report := range []string{ReportChanges, ReportLongJobs, ""} {
		if _, err := saveNotifications(t.Context(), NotificationSettings{Scripts: map[string]string{"a": report}}); err == nil {
			t.Errorf("override %q accepted", report)
		}
	}
}

func TestSaveWritesWhatAnOlderBuildReads(t *testing.T) {
	browser := Channels{Browser: true}
	mailed := Channels{Browser: true, Email: true}
	for name, tc := range map[string]struct {
		levels map[string]Channels
		docker string
		want   olderBuild
	}{
		"email for warnings and alerts": {
			levels: map[string]Channels{SeverityInfo: browser, SeverityWarning: mailed, SeverityError: mailed},
			docker: ReportFailures,
			want:   olderBuild{MinSeverity: SeverityWarning, DockerUpdates: DockerUpdateOnFailure},
		},
		"webhook for alerts": {
			levels: map[string]Channels{SeverityInfo: browser, SeverityWarning: browser, SeverityError: {Browser: true, Webhook: true}},
			docker: ReportChanges,
			want:   olderBuild{MinSeverity: SeverityError, DockerUpdates: DockerUpdateOnChange},
		},
		"nothing sent": {
			levels: map[string]Channels{SeverityInfo: browser, SeverityWarning: browser, SeverityError: browser},
			docker: ReportEveryRun,
			want:   olderBuild{MinSeverity: "", DockerUpdates: DockerUpdateOnChange},
		},
	} {
		t.Run(name, func(t *testing.T) {
			useTempStore(t)
			// The older members say otherwise; what is stored for the levels and the report wins.
			_, err := saveNotifications(t.Context(), NotificationSettings{
				MinSeverity: SeverityInfo, DockerUpdates: DockerUpdateNever,
				Levels: tc.levels, Report: map[string]string{KindDockerUpdates: tc.docker},
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := readAsOlderBuild(t); got != tc.want {
				t.Fatalf("an older build reads %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestUpdateNotificationsKeepsTheOtherWritersChange(t *testing.T) {
	useTempStore(t)
	ctx := t.Context()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseFirst := func() { releaseOnce.Do(func() { close(release) }) }
	var writers sync.WaitGroup
	// A failing run still releases the first writer and waits for both.
	t.Cleanup(func() {
		releaseFirst()
		writers.Wait()
	})
	first := make(chan error, 1)
	writers.Go(func() {
		_, err := UpdateNotifications(ctx, func(n *NotificationSettings) error {
			close(entered)
			<-release
			n.Scripts = map[string]string{"a": ReportEveryRun}
			return nil
		})
		first <- err
	})
	<-entered
	second := make(chan error, 1)
	writers.Go(func() {
		_, err := UpdateNotifications(ctx, func(n *NotificationSettings) error {
			n.WebhookURL = "https://hooks.example.com/x"
			return nil
		})
		second <- err
	})
	select {
	case err := <-second:
		t.Fatalf("the second writer did not wait for the first: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	releaseFirst()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	n := loadNotifications(t)
	if n.ReportForScript("a") != ReportEveryRun || n.WebhookURL != "https://hooks.example.com/x" {
		t.Fatalf("a change was lost: scripts = %v, webhook = %q", n.Scripts, n.WebhookURL)
	}
}

func TestUpdateNotificationsRefusesAnUnreadableFile(t *testing.T) {
	for name, content := range map[string]string{
		"not JSON":           `{not json`,
		"value out of range": `{"smtpPort": 31415926535897932384626}`,
	} {
		t.Run(name, func(t *testing.T) {
			useTempStore(t)
			writeNotificationsFile(t, content)
			_, err := UpdateNotifications(t.Context(), func(n *NotificationSettings) error {
				n.WebhookURL = "https://hooks.example.com/x"
				return nil
			})
			if !errors.Is(err, ErrUnparsableSettings) {
				t.Fatalf("err = %v, want the parse error", err)
			}
			if strings.Contains(err.Error(), "31415926535897932384626") || !strings.Contains(err.Error(), notificationsPath()) {
				t.Fatalf("err = %v: it must name the file and not quote it", err)
			}
			if data, _ := os.ReadFile(notificationsPath()); string(data) != content {
				t.Fatalf("file = %q, want it as it was", data)
			}
		})
	}
}

func TestNotificationsOrDefaults(t *testing.T) {
	useTempStore(t)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	writeNotificationsFile(t, `{"report":{"scheduled_scripts":"every_run"},"smtpPassword":"hunter2"}`)
	if got := NotificationsOrDefaults(); got.ReportFor(KindScripts) != ReportEveryRun || logs.Len() != 0 {
		t.Errorf("readable settings: scripts report %q, logs %q; want every_run and nothing logged", got.ReportFor(KindScripts), logs.String())
	}

	writeNotificationsFile(t, `{"smtpPassword":"hunter2",`)
	if got := NotificationsOrDefaults(); !reflect.DeepEqual(got, DefaultNotifications()) {
		t.Errorf("unreadable settings = %+v, want the defaults", got)
	}
	line := logs.String()
	if strings.Count(line, "level=WARN") != 1 || !strings.Contains(line, "error=") || strings.Contains(line, "hunter2") {
		t.Errorf("logs = %q, want one warning with the error and nothing of the file", line)
	}
}

func TestNotificationsMayName(t *testing.T) {
	useTempStore(t)
	if NotificationsMayName("abc") {
		t.Error("a missing file may name nothing")
	}
	if err := os.WriteFile(notificationsPath(), []byte(`{"scripts": {"abc": "failures"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !NotificationsMayName("abc") || NotificationsMayName("other") {
		t.Error("an unparsable file may name only what it contains")
	}
}
