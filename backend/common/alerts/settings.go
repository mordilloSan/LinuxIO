package alerts

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
)

// NotificationSettings are what each kind of work reports and where each
// level of alert goes: the browser, a JSON webhook and email over SMTP. The
// file sits beside the alert store, root 0600, since it holds the SMTP
// password. It is the persisted form, never a wire type.
type NotificationSettings struct {
	// MinSeverity is the least severe alert delivered: info, warning or
	// error. Empty delivers nothing. Levels replaces it; it is read for a
	// level Levels does not name and written for an older build.
	MinSeverity string `json:"minSeverity"`
	// DockerUpdates is what the scheduled Docker update worker reports:
	// never, on_failure (the default) or on_change. Report replaces it; it is
	// read when Report does not name Docker updates and written for an older
	// build.
	DockerUpdates string `json:"dockerUpdates"`
	// DockerUpdatesAvailable also reports every container whose image has a
	// newer version after each scheduled check.
	DockerUpdatesAvailable bool                `json:"dockerUpdatesAvailable"`
	Report                 map[string]string   `json:"report,omitempty"`  // kind -> report; ReportFor says what a missing kind reports
	Scripts                map[string]string   `json:"scripts,omitempty"` // schedule ID -> report, overriding the scheduled scripts' line
	Levels                 map[string]Channels `json:"levels,omitempty"`  // severity -> channels
	WebhookURL             string              `json:"webhookUrl"`
	SMTPHost               string              `json:"smtpHost"`
	SMTPPort               int                 `json:"smtpPort"`
	SMTPUsername           string              `json:"smtpUsername"`
	SMTPPassword           string              `json:"smtpPassword,omitempty"`
	EmailFrom              string              `json:"emailFrom"`
	EmailTo                []string            `json:"emailTo"`
}

// Channels says where the alerts of one level go.
type Channels struct {
	Browser bool `json:"browser"`
	Email   bool `json:"email"`
	Webhook bool `json:"webhook"`
}

// What a kind of work reports besides its failures, which are always reported,
// and the kinds of work. LongJob is how long a job started in the browser runs
// before long_jobs reports it.
const (
	ReportFailures = "failures"
	ReportChanges  = "changes"
	ReportEveryRun = "every_run"
	ReportLongJobs = "long_jobs"

	KindDockerUpdates = "docker_updates"
	KindScripts       = "scheduled_scripts"
	KindSnapshots     = "snapshots"
	KindResync        = "resync"
	KindBrowserJobs   = "browser_jobs"

	LongJob = 5 * time.Minute
)

// Docker update policies; DockerUpdateOnFailure is what an unset value means.
const (
	DockerUpdateNever     = "never"
	DockerUpdateOnFailure = "on_failure"
	DockerUpdateOnChange  = "on_change"
)

var severityRank = map[string]int{SeverityInfo: 0, SeverityWarning: 1, SeverityError: 2}

// reportChoices are the reports each kind offers: only those that can differ
// for it.
var reportChoices = map[string][]string{
	KindDockerUpdates: {ReportFailures, ReportChanges, ReportEveryRun},
	KindScripts:       {ReportFailures, ReportEveryRun},
	KindSnapshots:     {ReportFailures, ReportChanges, ReportEveryRun},
	KindResync:        {ReportFailures, ReportChanges},
	KindBrowserJobs:   {ReportFailures, ReportLongJobs},
}

// ReportOffered says whether kind offers report, as a scheduled script's own
// choice must.
func ReportOffered(kind, report string) bool { return slices.Contains(reportChoices[kind], report) }

func notificationsPath() string { return filepath.Join(filepath.Dir(Path), "notifications.json") }

// NotificationsMayName reports whether the stored notification settings could
// hold a setting under key: false only when the file is missing or does not
// contain key at all. A caller that cannot parse the settings uses it to tell
// whether there is anything to clear.
func NotificationsMayName(key string) bool {
	data, err := os.ReadFile(notificationsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	return err != nil || bytes.Contains(data, []byte(key))
}

var (
	// ErrUnparsableSettings is a settings file that is there but does not parse.
	ErrUnparsableSettings = errors.New("not valid notification settings")
	// ErrInvalidSettings wraps what validation refuses in a save.
	ErrInvalidSettings = errors.New("invalid notification settings")
)

// LoadNotifications reads the settings, naming every level and kind: what the
// file stores wins, then what its older members said, then the defaults. A
// missing file has the defaults.
func LoadNotifications() (NotificationSettings, error) {
	data, err := os.ReadFile(notificationsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return DefaultNotifications(), nil
	}
	if err != nil {
		return NotificationSettings{}, fmt.Errorf("read %s: %w", notificationsPath(), err)
	}
	var n NotificationSettings
	if err := json.Unmarshal(data, &n); err != nil {
		return NotificationSettings{}, parseError(err)
	}
	if n.EmailTo == nil {
		n.EmailTo = []string{}
	}
	if n.DockerUpdates == "" {
		n.DockerUpdates = DockerUpdateOnFailure
	}
	return n.complete(), nil
}

// DefaultNotifications are the settings before any are saved: every level
// reaches the browser, and errors also email and webhook once those are set up.
func DefaultNotifications() NotificationSettings {
	n := NotificationSettings{DockerUpdates: DockerUpdateOnFailure, EmailTo: []string{},
		Levels: map[string]Channels{SeverityError: {Browser: true, Email: true, Webhook: true}}}
	return n.complete()
}

// NotificationsOrDefaults is LoadNotifications for a producer deciding what
// to report: a file it cannot read is logged and the defaults apply, so the
// work it reports on never depends on the file. No report setting decides
// whether a failure or an interruption is raised; where an interruption that
// is a warning goes follows the levels grid like any other warning.
func NotificationsOrDefaults() NotificationSettings {
	n, err := LoadNotifications()
	if err != nil {
		slog.Warn("notification settings unreadable, using the defaults", "error", err)
		return DefaultNotifications()
	}
	return n
}

// parseError says where the file fails to parse without quoting it: it holds
// the SMTP password, and a json error may quote the value it rejects.
func parseError(err error) error {
	var syntactic *jsontext.SyntacticError
	var semantic *json.SemanticError
	switch {
	case errors.As(err, &syntactic):
		return fmt.Errorf("parse %s: %w at offset %d", notificationsPath(), ErrUnparsableSettings, syntactic.ByteOffset)
	case errors.As(err, &semantic):
		return fmt.Errorf("parse %s: %w at offset %d", notificationsPath(), ErrUnparsableSettings, semantic.ByteOffset)
	}
	return fmt.Errorf("parse %s: %w", notificationsPath(), ErrUnparsableSettings)
}

// UpdateNotifications loads the settings, applies fn and saves, under one lock.
// What fn leaves is validated, and stored with the older members set to what
// an older build would act on. An empty password keeps the stored one unless
// the username was cleared. A file that does not parse is an error and is left
// as it is.
func UpdateNotifications(ctx context.Context, fn func(*NotificationSettings) error) (NotificationSettings, error) {
	return updateNotifications(ctx, fn, false)
}

// RewriteNotifications is UpdateNotifications for a writer that sets every
// setting it owns, as the settings page does: over a file that does not parse
// it starts from the defaults. That file may hold the only copy of the
// password, the webhook URL and the script overrides, so a save that is about
// to replace it first copies it to notifications.json.unreadable beside it,
// root 0600, replacing an older one; a copy that fails fails the save, and a
// save that is refused leaves the file as it was.
func RewriteNotifications(ctx context.Context, fn func(*NotificationSettings) error) (NotificationSettings, error) {
	return updateNotifications(ctx, fn, true)
}

func updateNotifications(ctx context.Context, fn func(*NotificationSettings) error, overUnparsable bool) (NotificationSettings, error) {
	var saved NotificationSettings
	err := filelock.RunExclusive(ctx, notificationsPath()+".lock", func() error {
		n, loadErr := LoadNotifications()
		replacing := overUnparsable && errors.Is(loadErr, ErrUnparsableSettings)
		switch {
		case replacing:
			n = DefaultNotifications()
		case loadErr != nil:
			return loadErr
		}
		password := n.SMTPPassword
		if err := fn(&n); err != nil {
			return err
		}
		n, err := n.prepare(password)
		if err != nil {
			return err
		}
		data, err := json.Marshal(n, jsontext.WithIndent("  "), json.Deterministic(true))
		if err != nil {
			return fmt.Errorf("marshal notifications: %w", err)
		}
		var kept string
		if replacing {
			if kept, err = keepUnparsable(); err != nil {
				return err
			}
		}
		if err := atomicfile.Write(notificationsPath(), append(data, '\n'), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", notificationsPath(), err)
		}
		if replacing {
			slog.Warn("notification settings unreadable, starting from the defaults", "path", notificationsPath(), "kept_as", kept, "error", loadErr)
		}
		saved = n
		return nil
	}, filelock.WithTimeout(lockWait))
	if err != nil {
		return NotificationSettings{}, err
	}
	return saved, nil
}

// keepUnparsable copies the settings file, which does not parse, to
// notifications.json.unreadable, 0600, replacing an older one, and returns
// that path. The caller holds the settings lock and replaces the file next.
func keepUnparsable() (string, error) {
	kept := notificationsPath() + ".unreadable"
	data, err := os.ReadFile(notificationsPath())
	if err != nil {
		return "", fmt.Errorf("keep unreadable notification settings: %w", err)
	}
	if err := atomicfile.Write(kept, data, 0o600); err != nil {
		return "", fmt.Errorf("keep unreadable notification settings: %w", err)
	}
	return kept, nil
}

// prepare tidies and validates n for storing, keeping the stored password
// when n carries none, and writes the older members from the levels and the
// Docker report. The Docker policy stays as given when Report names no Docker
// report, as it does when the settings are read.
func (n NotificationSettings) prepare(storedPassword string) (NotificationSettings, error) {
	n.WebhookURL, n.SMTPHost, n.SMTPUsername = strings.TrimSpace(n.WebhookURL), strings.TrimSpace(n.SMTPHost), strings.TrimSpace(n.SMTPUsername)
	n.EmailFrom = strings.TrimSpace(n.EmailFrom)
	if n.SMTPPort == 0 {
		n.SMTPPort = 587
	}
	if n.DockerUpdates == "" {
		n.DockerUpdates = DockerUpdateOnFailure
	}
	if err := n.validate(); err != nil {
		return NotificationSettings{}, fmt.Errorf("%w: %w", ErrInvalidSettings, err)
	}
	if n.SMTPPassword == "" {
		n.SMTPPassword = storedPassword
	}
	if n.SMTPUsername == "" {
		n.SMTPPassword = ""
	}
	if report, ok := n.Report[KindDockerUpdates]; ok {
		n.DockerUpdates = DockerUpdateOnFailure
		if report == ReportChanges || report == ReportEveryRun {
			n.DockerUpdates = DockerUpdateOnChange
		}
	}
	n = n.complete()
	n.MinSeverity = ""
	for severity, c := range n.Levels {
		if (c.Email || c.Webhook) && (n.MinSeverity == "" || severityRank[severity] < severityRank[n.MinSeverity]) {
			n.MinSeverity = severity
		}
	}
	return n, nil
}

// complete names every level and kind in Levels and Report. A stored report
// or script override that its kind does not offer, as a hand edit or a newer
// build before a downgrade may leave, is dropped and takes its default rather
// than failing every later save.
func (n NotificationSettings) complete() NotificationSettings {
	n.Report, n.Scripts = maps.Clone(n.Report), maps.Clone(n.Scripts)
	maps.DeleteFunc(n.Report, func(kind, report string) bool { return !ReportOffered(kind, report) })
	maps.DeleteFunc(n.Scripts, func(_, report string) bool { return !ReportOffered(KindScripts, report) })
	levels := make(map[string]Channels, len(severityRank))
	for severity := range severityRank {
		levels[severity] = n.ChannelsFor(severity)
	}
	report := make(map[string]string, len(reportChoices))
	for kind := range reportChoices {
		report[kind] = n.ReportFor(kind)
	}
	n.Levels, n.Report = levels, report
	return n
}

// ReportFor is what kind reports besides its failures. A kind Report does not
// name reports failures only, except that Docker updates follow the older
// Docker policy and a pool resync also reports a pool that is whole again, as
// it always has.
func (n NotificationSettings) ReportFor(kind string) string {
	if report, ok := n.Report[kind]; ok {
		return report
	}
	if kind == KindResync || (kind == KindDockerUpdates && n.DockerUpdates == DockerUpdateOnChange) {
		return ReportChanges
	}
	return ReportFailures
}

// ReportForScript is what the scheduled script scheduleID reports: its own
// override, or else the scheduled scripts' line.
func (n NotificationSettings) ReportForScript(scheduleID string) string {
	if report, ok := n.Scripts[scheduleID]; ok {
		return report
	}
	return n.ReportFor(KindScripts)
}

// ChannelsFor says where alerts of severity go. A level Levels does not name
// reaches the browser, and email and webhook from the older least severity
// on. Errors always reach the browser.
func (n NotificationSettings) ChannelsFor(severity string) Channels {
	c, ok := n.Levels[severity]
	if !ok {
		sent := n.MinSeverity != "" && severityRank[severity] >= severityRank[n.MinSeverity]
		c = Channels{Browser: true, Email: sent, Webhook: sent}
	}
	if severity == SeverityError {
		c.Browser = true
	}
	return c
}

func (n NotificationSettings) validate() error {
	if _, ok := severityRank[n.MinSeverity]; !ok && n.MinSeverity != "" {
		return fmt.Errorf("invalid minimum severity %q", n.MinSeverity)
	}
	switch n.DockerUpdates {
	case DockerUpdateNever, DockerUpdateOnFailure, DockerUpdateOnChange:
	default:
		return fmt.Errorf("invalid Docker update policy %q", n.DockerUpdates)
	}
	if err := n.validateReports(); err != nil {
		return err
	}
	if n.WebhookURL != "" {
		u, err := url.Parse(n.WebhookURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return fmt.Errorf("webhook URL must be an http or https URL")
		}
	}
	if n.SMTPHost == "" {
		return nil
	}
	if strings.ContainsAny(n.SMTPHost, " /:") {
		return fmt.Errorf("invalid SMTP host %q", n.SMTPHost)
	}
	if n.SMTPPort < 1 || n.SMTPPort > 65535 {
		return fmt.Errorf("invalid SMTP port %d", n.SMTPPort)
	}
	if _, err := mail.ParseAddress(n.EmailFrom); err != nil {
		return fmt.Errorf("invalid sender address %q", n.EmailFrom)
	}
	if len(n.EmailTo) == 0 {
		return errors.New("at least one recipient is required")
	}
	for _, to := range n.EmailTo {
		if _, err := mail.ParseAddress(to); err != nil {
			return fmt.Errorf("invalid recipient address %q", to)
		}
	}
	return nil
}

// validateReports accepts only the reports each kind offers and the levels
// that are severities.
func (n NotificationSettings) validateReports() error {
	for kind, report := range n.Report {
		choices, ok := reportChoices[kind]
		if !ok {
			return fmt.Errorf("unknown kind of work %q", kind)
		}
		if !slices.Contains(choices, report) {
			return fmt.Errorf("invalid report %q for %s", report, kind)
		}
	}
	for id, report := range n.Scripts {
		if !ReportOffered(KindScripts, report) {
			return fmt.Errorf("invalid report %q for scheduled script %q", report, id)
		}
	}
	for severity := range n.Levels {
		if !validSeverity(severity) {
			return fmt.Errorf("invalid alert level %q", severity)
		}
	}
	return nil
}
