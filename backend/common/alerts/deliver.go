package alerts

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// deliveryTimeout bounds one delivery to every channel.
const deliveryTimeout = 15 * time.Second

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
		// *url.Error text embeds the full URL, which carries the webhook secret.
		if urlErr, ok := errors.AsType[*url.Error](err); ok {
			return urlErr.Err
		}
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

// envelopeAddress returns the bare address of a validated "Name <addr>" form.
func envelopeAddress(s string) string {
	if addr, err := mail.ParseAddress(s); err == nil {
		return addr.Address
	}
	return s
}

// sendEmail sends a as a plain-text message to every recipient.
func sendEmail(ctx context.Context, n NotificationSettings, a Alert) error {
	c, err := dialSMTP(ctx, n)
	if err != nil {
		return err
	}
	defer c.Close()
	// The envelope takes the bare address; the headers keep the display name.
	if err = c.Mail(envelopeAddress(n.EmailFrom)); err != nil {
		return err
	}
	for _, to := range n.EmailTo {
		if err = c.Rcpt(envelopeAddress(to)); err != nil {
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
