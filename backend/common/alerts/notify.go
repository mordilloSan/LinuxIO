package alerts

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

// NotificationSettings are where new alerts are delivered besides the UI: a JSON
// webhook and email over SMTP. The file sits beside the alert store, root
// 0600, since it holds the SMTP password.
type NotificationSettings struct {
	// MinSeverity is the least severe alert delivered: info, warning or
	// error. Empty delivers nothing.
	MinSeverity  string   `json:"minSeverity"`
	WebhookURL   string   `json:"webhookUrl"`
	SMTPHost     string   `json:"smtpHost"`
	SMTPPort     int      `json:"smtpPort"`
	SMTPUsername string   `json:"smtpUsername"`
	SMTPPassword string   `json:"smtpPassword,omitempty"`
	EmailFrom    string   `json:"emailFrom"`
	EmailTo      []string `json:"emailTo"`
	// SMTPPasswordSet is reported instead of the password, which is never
	// sent back; a save with an empty password keeps the stored one.
	SMTPPasswordSet bool `json:"smtpPasswordSet"`
}

// deliveryTimeout bounds one delivery to every channel.
const deliveryTimeout = 15 * time.Second

var severityRank = map[string]int{SeverityInfo: 0, SeverityWarning: 1, SeverityError: 2}

func notificationsPath() string { return filepath.Join(filepath.Dir(Path), "notifications.json") }

// LoadNotifications reads the delivery settings; a missing file delivers nothing.
func LoadNotifications() (NotificationSettings, error) {
	data, err := os.ReadFile(notificationsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return NotificationSettings{EmailTo: []string{}}, nil
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
	n.SMTPPasswordSet = n.SMTPPassword != ""
	return n, nil
}

// View is n as the API shows it: without the password.
func (n NotificationSettings) View() NotificationSettings {
	n.SMTPPasswordSet, n.SMTPPassword = n.SMTPPassword != "", ""
	return n
}

// SaveNotifications validates and stores n. An empty password keeps the
// stored one unless the username was cleared.
func SaveNotifications(n NotificationSettings) (NotificationSettings, error) {
	n.WebhookURL, n.SMTPHost, n.SMTPUsername = strings.TrimSpace(n.WebhookURL), strings.TrimSpace(n.SMTPHost), strings.TrimSpace(n.SMTPUsername)
	n.EmailFrom = strings.TrimSpace(n.EmailFrom)
	if n.SMTPPort == 0 {
		n.SMTPPort = 587
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
	n.SMTPPasswordSet = false
	data, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return NotificationSettings{}, fmt.Errorf("marshal notifications: %w", err)
	}
	if err := utils.WriteFileAtomic(notificationsPath(), append(data, '\n'), 0o600); err != nil {
		return NotificationSettings{}, fmt.Errorf("write %s: %w", notificationsPath(), err)
	}
	return n.View(), nil
}

func (n NotificationSettings) validate() error {
	if _, ok := severityRank[n.MinSeverity]; !ok && n.MinSeverity != "" {
		return fmt.Errorf("invalid minimum severity %q", n.MinSeverity)
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

// notify delivers a new or changed alert to the configured channels.
// Failures are logged: the alert itself is already recorded.
func notify(ctx context.Context, a Alert) {
	n, err := LoadNotifications()
	if err != nil {
		slog.Warn("alert delivery settings unreadable", "error", err)
		return
	}
	if !n.wants(a.Severity) {
		return
	}
	if err := Send(ctx, n, a); err != nil {
		slog.Warn("alert delivery failed", "alert", a.ID, "error", err)
	}
}

// Send delivers a to every configured channel, bounded by deliveryTimeout
// even when ctx is already cancelled, and joins their errors.
func Send(ctx context.Context, n NotificationSettings, a Alert) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deliveryTimeout)
	defer cancel()
	var errs []error
	if n.WebhookURL != "" {
		if err := postWebhook(ctx, n.WebhookURL, a); err != nil {
			errs = append(errs, fmt.Errorf("webhook: %w", err))
		}
	}
	if n.SMTPHost != "" && len(n.EmailTo) > 0 {
		if err := sendEmail(ctx, n, a); err != nil {
			errs = append(errs, fmt.Errorf("email: %w", err))
		}
	}
	return errors.Join(errs...)
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "linuxio"
	}
	return h
}

// postWebhook posts the alert as JSON; text and content repeat it as one
// line for Slack- and Discord-style receivers.
func postWebhook(ctx context.Context, target string, a Alert) error {
	line := fmt.Sprintf("[%s] %s: %s", hostname(), strings.ToUpper(a.Severity), a.Title)
	body, err := json.Marshal(map[string]string{
		"host": hostname(), "source": a.Source, "severity": a.Severity, "title": a.Title,
		"message": a.Message, "link": a.Link, "time": a.LastOccurrence.UTC().Format(time.RFC3339),
		"text": line, "content": line,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s answered %s", req.URL.Redacted(), resp.Status)
	}
	return nil
}

// oneLine keeps a header value on one line, so alert text cannot add headers.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// dialSMTP connects to the server, with implicit TLS on port 465 and
// otherwise STARTTLS when offered, and logs in when a username is set. A
// password is never sent without TLS.
func dialSMTP(ctx context.Context, n NotificationSettings) (*smtp.Client, error) {
	addr := net.JoinHostPort(n.SMTPHost, strconv.Itoa(n.SMTPPort))
	tlsConfig := &tls.Config{ServerName: n.SMTPHost, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	var err error
	if n.SMTPPort == 465 {
		conn, err = (&tls.Dialer{Config: tlsConfig}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	c, err := smtp.NewClient(conn, n.SMTPHost)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if ok, _ := c.Extension("STARTTLS"); ok && n.SMTPPort != 465 {
		err = c.StartTLS(tlsConfig)
	}
	if err == nil && n.SMTPUsername != "" {
		err = c.Auth(smtp.PlainAuth("", n.SMTPUsername, n.SMTPPassword, n.SMTPHost))
	}
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// sendEmail sends a as a plain-text message to every recipient.
func sendEmail(ctx context.Context, n NotificationSettings, a Alert) error {
	c, err := dialSMTP(ctx, n)
	if err != nil {
		return err
	}
	defer c.Close()
	if err = c.Mail(n.EmailFrom); err != nil {
		return err
	}
	for _, to := range n.EmailTo {
		if err = c.Rcpt(to); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n\r\nSeverity: %s\r\nSource: %s\r\nHost: %s\r\n",
		n.EmailFrom, strings.Join(n.EmailTo, ", "), oneLine(fmt.Sprintf("[LinuxIO %s] %s", hostname(), a.Title)),
		a.LastOccurrence.Format(time.RFC1123Z), strings.ReplaceAll(a.Message, "\n", "\r\n"), a.Severity, a.Source, hostname())
	if _, err = w.Write([]byte(msg)); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// TestAlert is the alert a test delivery sends.
func TestAlert() Alert {
	t := now()
	return Alert{
		ID: ID("linuxio", "notification-test"), Source: "linuxio", Key: "notification-test", Severity: SeverityInfo,
		Title: "Test notification", Message: "Alert delivery from " + hostname() + " works.",
		FirstOccurrence: t, LastOccurrence: t, LastObservedAt: t, OccurrenceCount: 1,
	}
}
