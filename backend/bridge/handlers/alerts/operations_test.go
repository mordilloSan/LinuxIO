package alerts

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	store "github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

func useTempStore(t *testing.T) {
	t.Helper()
	old := store.Path
	store.Path = filepath.Join(t.TempDir(), "alerts.json")
	t.Cleanup(func() { store.Path = old })
}

func writeCorrupt(path string) error {
	return os.WriteFile(path, []byte("{"), 0o600)
}

func TestListFiltersDismissedAndComputesSeenPerUID(t *testing.T) {
	useTempStore(t)
	ctx := context.Background()
	_ = store.Raise(ctx, store.Observation{Source: "s", Key: "a", Severity: "info", Title: "A", OccurrenceID: "1"})
	_ = store.Raise(ctx, store.Observation{Source: "s", Key: "b", Severity: "error", Title: "B", OccurrenceID: "1"})
	_ = store.Raise(ctx, store.Observation{Source: "s", Key: "c", Severity: "info", Title: "C", OccurrenceID: "1"})
	_ = store.Dismiss(ctx, "s/c")
	_ = store.MarkSeen(ctx, "1000", []string{"s/a"})

	list, err := listForUID(1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Alerts) != 2 {
		t.Fatalf("alerts = %+v", list.Alerts)
	}
	if list.Unseen != 1 {
		t.Fatalf("unseen = %d", list.Unseen)
	}
	for _, a := range list.Alerts {
		if a.ID == "s/a" && !a.Seen || a.ID == "s/b" && a.Seen {
			t.Fatalf("seen wrong: %+v", a)
		}
		if a.FirstOccurrence == "" || a.LastOccurrence == "" {
			t.Fatalf("timestamps missing: %+v", a)
		}
	}
	other, _ := listForUID(1001)
	if other.Unseen != 2 {
		t.Fatalf("other uid unseen = %d", other.Unseen)
	}
}

func TestMarkAllSeenAndDismissReturnFreshList(t *testing.T) {
	useTempStore(t)
	ctx := context.Background()
	_ = store.Raise(ctx, store.Observation{Source: "s", Key: "a", Severity: "info", Title: "A"})
	_ = store.Raise(ctx, store.Observation{Source: "s", Key: "b", Severity: "info", Title: "B"})
	list, err := markSeenForUID(ctx, 1000, nil)
	if err != nil || list.Unseen != 0 {
		t.Fatalf("list = %+v err = %v", list, err)
	}
	list, err = dismissForUID(ctx, 1000, "s/a")
	if err != nil || len(list.Alerts) != 1 || list.Alerts[0].ID != "s/b" {
		t.Fatalf("list = %+v err = %v", list, err)
	}
	if _, err := dismissForUID(ctx, 1000, "s/missing"); err == nil {
		t.Fatal("dismiss of unknown id succeeded")
	}
}

func TestListSurfacesCorruptStore(t *testing.T) {
	useTempStore(t)
	if err := writeCorrupt(store.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := listForUID(1000); err == nil {
		t.Fatal("corrupt store listed as empty")
	}
}

func TestNotificationsViewOmitsPassword(t *testing.T) {
	n := store.NotificationSettings{SMTPUsername: "nas", SMTPPassword: "secret", EmailTo: []string{"me@example.com"}}
	view := notificationsView(n)
	if !view.SMTPPasswordSet || view.SMTPUsername != "nas" {
		t.Fatalf("view = %+v", view)
	}
	if data, err := json.Marshal(view); err != nil || strings.Contains(string(data), "secret") {
		t.Fatalf("view leaks the password: %s, %v", data, err)
	}
	if got := notificationsFromRequest(apischema.NotificationSettings{SMTPPassword: "p"}, store.NotificationSettings{}); got.SMTPPassword != "p" {
		t.Fatalf("request password dropped: %+v", got)
	}
}

func notificationsFile() string { return filepath.Join(filepath.Dir(store.Path), "notifications.json") }

// storeSettings writes settings the way another writer (a schedule's dialog,
// a newer page) would.
func storeSettings(t *testing.T, fn func(n *store.NotificationSettings)) {
	t.Helper()
	if _, err := store.UpdateNotifications(t.Context(), func(n *store.NotificationSettings) error {
		fn(n)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func loadSettings(t *testing.T) store.NotificationSettings {
	t.Helper()
	n, err := store.LoadNotifications()
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSettingsSaveKeepsScriptOverrides(t *testing.T) {
	useTempStore(t)
	ctx := t.Context()
	storeSettings(t, func(n *store.NotificationSettings) {
		n.Scripts = map[string]string{"a": store.ReportEveryRun}
		n.Report = map[string]string{store.KindSnapshots: store.ReportChanges}
	})

	// An older settings page sends neither report nor levels.
	var older apischema.NotificationSettings
	if err := json.Unmarshal([]byte(`{"dockerUpdates": "on_change", "minSeverity": "", "webhookUrl": "https://hooks.example.com/x", "emailTo": []}`), &older); err != nil {
		t.Fatal(err)
	}
	saved, err := saveNotifications(ctx, older)
	if err != nil {
		t.Fatal(err)
	}
	n := loadSettings(t)
	if n.ReportForScript("a") != store.ReportEveryRun || n.ReportFor(store.KindSnapshots) != store.ReportChanges || n.WebhookURL != "https://hooks.example.com/x" {
		t.Fatalf("stored = %+v", n)
	}
	if view := notificationsView(saved); view.Report[store.KindSnapshots] != store.ReportChanges || view.Report[store.KindDockerUpdates] != store.ReportChanges {
		t.Fatalf("view = %+v", view)
	}

	// A page that sends them replaces them, and still keeps the script overrides.
	saved, err = saveNotifications(ctx, apischema.NotificationSettings{
		Report: map[string]string{store.KindDockerUpdates: store.ReportEveryRun},
		Levels: map[string]apischema.NotificationChannels{store.SeverityWarning: {Browser: true, Webhook: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	n = loadSettings(t)
	if n.ReportForScript("a") != store.ReportEveryRun || n.ReportFor(store.KindDockerUpdates) != store.ReportEveryRun || n.ReportFor(store.KindSnapshots) != store.ReportFailures {
		t.Fatalf("stored = %+v", n)
	}
	if got := n.ChannelsFor(store.SeverityWarning); got != (store.Channels{Browser: true, Webhook: true}) {
		t.Fatalf("warning = %+v", got)
	}
	if view := notificationsView(saved); view.Levels[store.SeverityWarning] != (apischema.NotificationChannels{Browser: true, Webhook: true}) || view.Report[store.KindDockerUpdates] != store.ReportEveryRun {
		t.Fatalf("view = %+v", view)
	}
}

func TestSaveFromAnOlderPageSetsTheLevels(t *testing.T) {
	useTempStore(t)
	storeSettings(t, func(n *store.NotificationSettings) {
		n.Levels = map[string]store.Channels{store.SeverityInfo: {Browser: true, Email: true}}
	})
	if _, err := saveNotifications(t.Context(), apischema.NotificationSettings{MinSeverity: store.SeverityError}); err != nil {
		t.Fatal(err)
	}
	n := loadSettings(t)
	for severity, want := range map[string]store.Channels{
		store.SeverityInfo:    {Browser: true},
		store.SeverityWarning: {Browser: true},
		store.SeverityError:   {Browser: true, Email: true, Webhook: true},
	} {
		if got := n.ChannelsFor(severity); got != want {
			t.Errorf("%s = %+v, want %+v", severity, got, want)
		}
	}
	if n.MinSeverity != store.SeverityError {
		t.Errorf("minSeverity = %q", n.MinSeverity)
	}
}

func TestSaveFromAnOlderPageSetsTheDockerReport(t *testing.T) {
	useTempStore(t)
	storeSettings(t, func(n *store.NotificationSettings) {
		n.Report = map[string]string{store.KindScripts: store.ReportEveryRun}
	})
	if _, err := saveNotifications(t.Context(), apischema.NotificationSettings{DockerUpdates: store.DockerUpdateOnChange}); err != nil {
		t.Fatal(err)
	}
	n := loadSettings(t)
	if got := n.Report[store.KindDockerUpdates]; got != store.ReportChanges {
		t.Errorf("docker_updates = %q, want changes", got)
	}
	if got := n.Report[store.KindScripts]; got != store.ReportEveryRun {
		t.Errorf("scheduled_scripts = %q, want the stored every_run", got)
	}
	if n.DockerUpdates != store.DockerUpdateOnChange {
		t.Errorf("dockerUpdates = %q", n.DockerUpdates)
	}
}

func TestSaveWithLevelsIgnoresTheOldSetting(t *testing.T) {
	useTempStore(t)
	_, err := saveNotifications(t.Context(), apischema.NotificationSettings{
		MinSeverity: store.SeverityInfo, DockerUpdates: store.DockerUpdateOnChange,
		Levels: map[string]apischema.NotificationChannels{
			store.SeverityInfo:    {Browser: true},
			store.SeverityWarning: {Browser: true, Email: true},
			store.SeverityError:   {Browser: true, Email: true},
		},
		Report: map[string]string{store.KindDockerUpdates: store.ReportFailures},
	})
	if err != nil {
		t.Fatal(err)
	}
	n := loadSettings(t)
	if got := n.ChannelsFor(store.SeverityInfo); got != (store.Channels{Browser: true}) {
		t.Errorf("info = %+v, want the sent channels", got)
	}
	if n.MinSeverity != store.SeverityWarning {
		t.Errorf("minSeverity = %q, want warning from the levels", n.MinSeverity)
	}
	if n.ReportFor(store.KindDockerUpdates) != store.ReportFailures || n.DockerUpdates != store.DockerUpdateOnFailure {
		t.Errorf("docker = %q / %q, want the sent report", n.ReportFor(store.KindDockerUpdates), n.DockerUpdates)
	}
}

// A sent map decides alone: a level it leaves out takes its default, not
// what the request's minSeverity would give it.
func TestSaveWithSomeLevelsGivesTheOthersTheirDefaults(t *testing.T) {
	useTempStore(t)
	if _, err := saveNotifications(t.Context(), apischema.NotificationSettings{
		MinSeverity: store.SeverityInfo,
		Levels:      map[string]apischema.NotificationChannels{store.SeverityWarning: {Browser: true, Email: true}},
	}); err != nil {
		t.Fatal(err)
	}
	n := loadSettings(t)
	for severity, want := range map[string]store.Channels{
		store.SeverityInfo:    {Browser: true},
		store.SeverityWarning: {Browser: true, Email: true},
		store.SeverityError:   {Browser: true, Email: true, Webhook: true},
	} {
		if got := n.ChannelsFor(severity); got != want {
			t.Errorf("%s = %+v, want %+v", severity, got, want)
		}
	}
}

// A sent report decides alone: Docker updates it leaves out take their
// default, not what the request's dockerUpdates would give them.
func TestSaveWithAReportGivesDockerUpdatesTheirDefault(t *testing.T) {
	useTempStore(t)
	if _, err := saveNotifications(t.Context(), apischema.NotificationSettings{
		DockerUpdates: store.DockerUpdateOnChange,
		Report:        map[string]string{store.KindSnapshots: store.ReportChanges},
	}); err != nil {
		t.Fatal(err)
	}
	n := loadSettings(t)
	if got := n.ReportFor(store.KindDockerUpdates); got != store.ReportFailures || n.DockerUpdates != store.DockerUpdateOnFailure {
		t.Errorf("docker = %q / %q, want the default failures", got, n.DockerUpdates)
	}
	if got := n.ReportFor(store.KindSnapshots); got != store.ReportChanges {
		t.Errorf("snapshots = %q, want the sent changes", got)
	}
}

func TestSaveRefusesAnInvalidRequestAsInvalidArguments(t *testing.T) {
	useTempStore(t)
	for name, r := range map[string]apischema.NotificationSettings{
		"browser jobs every run": {Report: map[string]string{store.KindBrowserJobs: store.ReportEveryRun}},
		"unknown kind":           {Report: map[string]string{"file_indexer": store.ReportFailures}},
		"unknown level":          {Levels: map[string]apischema.NotificationChannels{"critical": {Browser: true}}},
		"webhook not http":       {WebhookURL: "ftp://x"},
	} {
		if _, err := saveNotifications(t.Context(), r); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
			t.Errorf("%s: err = %v, want ErrInvalidArgs", name, err)
		}
	}
}

// The page echoes the older members it was shown. Beside the levels and the
// report they decide nothing, so an invalid one, as a hand edit leaves, does
// not refuse the save; without their map they decide, and are checked.
func TestSaveIgnoresTheOlderMembersItsMapsReplace(t *testing.T) {
	useTempStore(t)
	if err := os.WriteFile(notificationsFile(), []byte(`{"minSeverity": "loud", "dockerUpdates": "sometimes", "levels": {
		"info": {"browser": true, "email": false, "webhook": false},
		"warning": {"browser": true, "email": true, "webhook": false},
		"error": {"browser": true, "email": true, "webhook": true}}, "report": {"docker_updates": "changes"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	view, err := getNotifications()
	if err != nil || view.MinSeverity != "loud" || view.DockerUpdates != "sometimes" {
		t.Fatalf("view = %+v, %v; want the hand edit shown", view, err)
	}
	if _, err := saveNotifications(t.Context(), apischema.NotificationSettings{
		MinSeverity: view.MinSeverity, DockerUpdates: view.DockerUpdates, EmailTo: view.EmailTo,
		Levels: view.Levels, Report: view.Report,
	}); err != nil {
		t.Fatalf("save = %v, want the older members ignored", err)
	}
	if n := loadSettings(t); n.MinSeverity != store.SeverityWarning || n.DockerUpdates != store.DockerUpdateOnChange {
		t.Errorf("older members = %q / %q, want warning / on_change from the levels and the report", n.MinSeverity, n.DockerUpdates)
	}

	for name, r := range map[string]apischema.NotificationSettings{
		"minSeverity without levels":   {MinSeverity: "loud", Report: view.Report},
		"dockerUpdates without report": {DockerUpdates: "sometimes", Levels: view.Levels},
	} {
		if _, err := saveNotifications(t.Context(), r); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
			t.Errorf("%s: err = %v, want ErrInvalidArgs", name, err)
		}
	}
}

// raise records an alert of severity as s/<key>.
func raise(t *testing.T, key, severity string) {
	t.Helper()
	if err := store.Raise(t.Context(), store.Observation{Source: "s", Key: key, Severity: severity, Title: key, OccurrenceID: "1"}); err != nil {
		t.Fatal(err)
	}
}

func listedIDs(list apischema.AlertList) []string {
	ids := make([]string, 0, len(list.Alerts))
	for _, a := range list.Alerts {
		ids = append(ids, a.ID)
	}
	slices.Sort(ids)
	return ids
}

func TestAlertListHidesLevelsWithoutTheBrowser(t *testing.T) {
	useTempStore(t)
	// No save writes errors out of the browser; a hand edit could, and they are listed anyway.
	if err := os.WriteFile(notificationsFile(), []byte(`{"levels": {
		"info": {"browser": false, "email": true, "webhook": false},
		"warning": {"browser": true, "email": false, "webhook": false},
		"error": {"browser": false, "email": false, "webhook": false}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	raise(t, "notice", store.SeverityInfo)
	raise(t, "warning", store.SeverityWarning)
	raise(t, "alert", store.SeverityError)
	list, err := listForUID(1000)
	if err != nil {
		t.Fatal(err)
	}
	if got := listedIDs(list); !slices.Equal(got, []string{"s/alert", "s/warning"}) || list.Unseen != 2 {
		t.Fatalf("listed %v with %d unseen, want the warning and the alert, both unseen", got, list.Unseen)
	}
}

// Settings that cannot be read hide nothing: failures still reach the bell.
func TestAlertListOverUnreadableSettingsShowsEveryLevel(t *testing.T) {
	useTempStore(t)
	raise(t, "notice", store.SeverityInfo)
	raise(t, "alert", store.SeverityError)
	if err := writeCorrupt(notificationsFile()); err != nil {
		t.Fatal(err)
	}
	logs := captureLogsAt(t, slog.LevelDebug)
	list, err := listForUID(1000)
	if err != nil {
		t.Fatal(err)
	}
	if got := listedIDs(list); !slices.Equal(got, []string{"s/alert", "s/notice"}) || list.Unseen != 2 {
		t.Fatalf("listed %v with %d unseen, want both", got, list.Unseen)
	}
	// The bell polls the list; the settings page warns once per load.
	var record map[string]any
	if json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &record) != nil || record["level"] != "DEBUG" || record["error"] == nil {
		t.Fatalf("logs = %q, want one debug record with the error", logs.String())
	}
}

func TestMarkAllSeenLeavesHiddenAlertsUnseen(t *testing.T) {
	useTempStore(t)
	storeSettings(t, func(n *store.NotificationSettings) { n.Levels[store.SeverityInfo] = store.Channels{} })
	raise(t, "notice", store.SeverityInfo)
	raise(t, "alert", store.SeverityError)
	list, err := markSeenForUID(t.Context(), 1000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := listedIDs(list); !slices.Equal(got, []string{"s/alert"}) || list.Unseen != 0 {
		t.Fatalf("listed %v with %d unseen, want the alert, seen", got, list.Unseen)
	}
	// Shown again, the notice nobody was shown is still unseen.
	storeSettings(t, func(n *store.NotificationSettings) { n.Levels[store.SeverityInfo] = store.Channels{Browser: true} })
	if list, err = listForUID(1000); err != nil {
		t.Fatal(err)
	}
	if list.Unseen != 1 {
		t.Fatalf("unseen = %d, want the notice", list.Unseen)
	}
	for _, a := range list.Alerts {
		if a.Seen != (a.ID == "s/alert") {
			t.Errorf("%s seen = %v", a.ID, a.Seen)
		}
	}
}

// smtpSink accepts one SMTP session without TLS or auth and sends its message
// on QUIT, before the reply, so it is there when the client's send returns.
func smtpSink(t *testing.T) (int, <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not TCP", ln.Addr())
	}
	msg := make(chan string, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		r := bufio.NewReader(conn)
		reply := func(s string) { _, _ = io.WriteString(conn, s+"\r\n") }
		reply("220 sink")
		var data strings.Builder
		for inData := false; ; {
			line, readErr := r.ReadString('\n')
			switch {
			case readErr != nil:
				return
			case inData && line == ".\r\n":
				inData = false
				reply("250 queued")
			case inData:
				data.WriteString(line)
			case strings.HasPrefix(line, "DATA"):
				inData = true
				reply("354 go")
			case strings.HasPrefix(line, "QUIT"):
				msg <- data.String()
				reply("221 bye")
				return
			default:
				reply("250 ok")
			}
		}
	}()
	return addr.Port, msg
}

func TestTestNotificationIgnoresTheGrid(t *testing.T) {
	useTempStore(t)
	var posts atomic.Int32
	hook := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { posts.Add(1) }))
	t.Cleanup(hook.Close)
	port, mailed := smtpSink(t)
	storeSettings(t, func(n *store.NotificationSettings) {
		n.WebhookURL, n.SMTPHost, n.SMTPPort = hook.URL, "127.0.0.1", port
		n.EmailFrom, n.EmailTo = "nas@example.com", []string{"me@example.com"}
		for severity := range n.Levels {
			n.Levels[severity] = store.Channels{Browser: true} // no level is emailed or posted
		}
	})
	if _, err := handleTestNotifications(t.Context(), apischema.NoRequest{}); err != nil {
		t.Fatal(err)
	}
	if got := posts.Load(); got != 1 {
		t.Errorf("webhook posts = %d, want 1", got)
	}
	select {
	case m := <-mailed:
		if !strings.Contains(m, "Test notification") {
			t.Errorf("email =\n%s", m)
		}
	default:
		t.Error("no email sent")
	}
}

// Over a file that does not parse the page gets the defaults and is told, so
// it can show the form and save over the file.
func TestGetNotificationsOverAnUnreadableFile(t *testing.T) {
	for name, content := range map[string]string{
		"not JSON":           `{not json`,
		"value out of range": `{"smtpPort": 31415926535897932384626}`,
	} {
		t.Run(name, func(t *testing.T) {
			useTempStore(t)
			if err := os.WriteFile(notificationsFile(), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			logs := captureLogs(t)
			view, err := getNotifications()
			if err != nil || !view.SettingsUnreadable {
				t.Fatalf("view = %+v, err = %v, want the defaults marked unreadable", view, err)
			}
			if view.Levels[store.SeverityError] != (apischema.NotificationChannels{Browser: true, Email: true, Webhook: true}) ||
				view.Levels[store.SeverityInfo] != (apischema.NotificationChannels{Browser: true}) || view.Report[store.KindResync] != store.ReportChanges {
				t.Fatalf("view = %+v, want the defaults", view)
			}
			if !strings.Contains(logs.String(), `"level":"WARN"`) || strings.Contains(logs.String(), "31415926535897932384626") {
				t.Fatalf("logs = %q, want one warning that does not quote the file", logs.String())
			}
		})
	}
}

func TestGetNotificationsOverAReadableFileIsNotUnreadable(t *testing.T) {
	useTempStore(t)
	if view, err := getNotifications(); err != nil || view.SettingsUnreadable {
		t.Fatalf("missing file: view = %+v, err = %v", view, err)
	}
	storeSettings(t, func(n *store.NotificationSettings) { n.WebhookURL = "https://hooks.example.com/x" })
	view, err := getNotifications()
	if err != nil || view.SettingsUnreadable || view.WebhookURL != "https://hooks.example.com/x" {
		t.Fatalf("stored file: view = %+v, err = %v", view, err)
	}
	if data, err := json.Marshal(view); err != nil || strings.Contains(string(data), "settingsUnreadable") {
		t.Fatalf("view = %s, %v: a readable file leaves the member out", data, err)
	}
}

// captureLogs sends slog's default logger to a buffer of JSON records for
// the rest of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	return captureLogsAt(t, slog.LevelInfo)
}

// captureLogsAt is captureLogs keeping the records of level and above.
func captureLogsAt(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: level})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &logs
}

// requireKept fails unless path holds content, readable by root only.
func requireKept(t *testing.T, path, content string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != content {
		t.Fatalf("%s = %q, %v; want %q", path, data, err, content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("%s mode = %v, want 0600", path, info.Mode())
	}
}

// saveWebhook saves url from the settings page and checks it is stored.
func saveWebhook(t *testing.T, url string) {
	t.Helper()
	if _, err := saveNotifications(t.Context(), apischema.NotificationSettings{WebhookURL: url}); err != nil {
		t.Fatalf("save over an unreadable file: %v", err)
	}
	if n := loadSettings(t); n.WebhookURL != url {
		t.Fatalf("stored = %+v", n)
	}
}

func TestSettingsSaveReplacesAnUnreadableFile(t *testing.T) {
	for name, content := range map[string]string{
		"not JSON":           `{not json`,
		"value out of range": `{"smtpPort": 31415926535897932384626}`,
	} {
		t.Run(name, func(t *testing.T) {
			useTempStore(t)
			// A hand edit may leave the file readable by others.
			if err := os.WriteFile(notificationsFile(), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			logs := captureLogs(t)

			saveWebhook(t, "https://hooks.example.com/x")
			// It may hold the only copy of the password and the choices: it is kept.
			kept := notificationsFile() + ".unreadable"
			requireKept(t, kept, content)
			var record map[string]any
			lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
			if len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &record) != nil {
				t.Fatalf("logs = %q, want one record", logs.String())
			}
			if record["level"] != "WARN" || record["path"] != notificationsFile() || record["kept_as"] != kept ||
				record["error"] == nil || strings.Contains(lines[0], "31415926535897932384626") {
				t.Fatalf("record = %v", record)
			}

			// A second save over another unparsable file keeps that one instead.
			if err := os.WriteFile(notificationsFile(), []byte(`{"again`), 0o600); err != nil {
				t.Fatal(err)
			}
			saveWebhook(t, "https://hooks.example.com/y")
			requireKept(t, kept, `{"again`)
		})
	}
}

// A save that is refused, or that cannot keep the old file, leaves the
// unparsable file where it is: the page keeps saying it could not be read.
func TestRefusedSaveLeavesAnUnreadableFileInPlace(t *testing.T) {
	const content = `{not json`
	kept := func() string { return notificationsFile() + ".unreadable" }

	t.Run("refused request", func(t *testing.T) {
		useTempStore(t)
		if err := os.WriteFile(notificationsFile(), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		logs := captureLogs(t)
		_, err := saveNotifications(t.Context(), apischema.NotificationSettings{WebhookURL: "not a url"})
		if bridgeipc.StatusCode(err) != 400 {
			t.Fatalf("err = %v, want a 400", err)
		}
		requireKept(t, notificationsFile(), content)
		if _, statErr := os.Stat(kept()); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("%s: %v, want no kept file", kept(), statErr)
		}
		if logs.Len() != 0 {
			t.Fatalf("logs = %q, want nothing about a save that did not happen", logs.String())
		}
	})

	t.Run("old file cannot be kept", func(t *testing.T) {
		useTempStore(t)
		if err := os.WriteFile(notificationsFile(), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(kept(), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := saveNotifications(t.Context(), apischema.NotificationSettings{WebhookURL: "https://hooks.example.com/x"}); err == nil {
			t.Fatal("the save succeeded without keeping the old file")
		}
		requireKept(t, notificationsFile(), content)
	})
}
