package alerts

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
)

// NotificationSettings are where new alerts are delivered besides the UI: a JSON
// webhook and email over SMTP. The file sits beside the alert store, root
// 0600, since it holds the SMTP password. It is the persisted form, never a
// wire type.
type NotificationSettings struct {
	// MinSeverity is the least severe alert delivered: info, warning or
	// error. Empty delivers nothing.
	MinSeverity string `json:"minSeverity"`
	// DockerUpdates is what the scheduled Docker update worker reports:
	// never, on_failure (the default) or on_change.
	DockerUpdates string `json:"dockerUpdates"`
	// DockerUpdatesAvailable also reports every container whose image has a
	// newer version after each scheduled check.
	DockerUpdatesAvailable bool     `json:"dockerUpdatesAvailable"`
	WebhookURL             string   `json:"webhookUrl"`
	SMTPHost               string   `json:"smtpHost"`
	SMTPPort               int      `json:"smtpPort"`
	SMTPUsername           string   `json:"smtpUsername"`
	SMTPPassword           string   `json:"smtpPassword,omitempty"`
	EmailFrom              string   `json:"emailFrom"`
	EmailTo                []string `json:"emailTo"`
}

// Docker update policies; DockerUpdateOnFailure is what an unset value means.
const (
	DockerUpdateNever     = "never"
	DockerUpdateOnFailure = "on_failure"
	DockerUpdateOnChange  = "on_change"
)

var severityRank = map[string]int{SeverityInfo: 0, SeverityWarning: 1, SeverityError: 2}

func notificationsPath() string { return filepath.Join(filepath.Dir(Path), "notifications.json") }

// LoadNotifications reads the delivery settings; a missing file delivers nothing.
func LoadNotifications() (NotificationSettings, error) {
	data, err := os.ReadFile(notificationsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return NotificationSettings{DockerUpdates: DockerUpdateOnFailure, EmailTo: []string{}}, nil
	}
	if err != nil {
		return NotificationSettings{}, fmt.Errorf("read %s: %w", notificationsPath(), err)
	}
	var n NotificationSettings
	if err := json.Unmarshal(data, &n); err != nil {
		return NotificationSettings{}, fmt.Errorf("parse %s: %w", notificationsPath(), err)
	}
	if n.EmailTo == nil {
		n.EmailTo = []string{}
	}
	if n.DockerUpdates == "" {
		n.DockerUpdates = DockerUpdateOnFailure
	}
	return n, nil
}

// SaveNotifications validates and stores n and returns what was stored. An
// empty password keeps the stored one unless the username was cleared.
func SaveNotifications(n NotificationSettings) (NotificationSettings, error) {
	n.WebhookURL, n.SMTPHost, n.SMTPUsername = strings.TrimSpace(n.WebhookURL), strings.TrimSpace(n.SMTPHost), strings.TrimSpace(n.SMTPUsername)
	n.EmailFrom = strings.TrimSpace(n.EmailFrom)
	if n.SMTPPort == 0 {
		n.SMTPPort = 587
	}
	if n.DockerUpdates == "" {
		n.DockerUpdates = DockerUpdateOnFailure
	}
	if err := n.validate(); err != nil {
		return NotificationSettings{}, err
	}
	if n.SMTPPassword == "" && n.SMTPUsername != "" {
		old, err := LoadNotifications()
		if err != nil {
			return NotificationSettings{}, err
		}
		n.SMTPPassword = old.SMTPPassword
	}
	if n.SMTPUsername == "" {
		n.SMTPPassword = ""
	}
	data, err := json.Marshal(n, jsontext.WithIndent("  "), json.Deterministic(true))
	if err != nil {
		return NotificationSettings{}, fmt.Errorf("marshal notifications: %w", err)
	}
	if err := atomicfile.Write(notificationsPath(), append(data, '\n'), 0o600); err != nil {
		return NotificationSettings{}, fmt.Errorf("write %s: %w", notificationsPath(), err)
	}
	return n, nil
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

// wants reports whether an alert of severity is delivered.
func (n NotificationSettings) wants(severity string) bool {
	return n.MinSeverity != "" && severityRank[severity] >= severityRank[n.MinSeverity]
}
