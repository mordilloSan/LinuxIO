package alerts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	store "github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

func uidKey(uid uint32) string { return strconv.FormatUint(uint64(uid), 10) }

func listForUID(uid uint32) (apischema.AlertList, error) {
	doc, err := store.Load()
	if err != nil {
		return apischema.AlertList{}, err
	}
	key := uidKey(uid)
	out := apischema.AlertList{Alerts: []apischema.Alert{}}
	for _, a := range doc.Alerts {
		if a.DismissedAt != nil {
			continue
		}
		seen := a.Seen[key] >= a.OccurrenceCount
		if !seen {
			out.Unseen++
		}
		out.Alerts = append(out.Alerts, apischema.Alert{
			ID: a.ID, Source: a.Source, Severity: a.Severity, Title: a.Title, Message: a.Message, Link: a.Link,
			OccurrenceCount: a.OccurrenceCount, Seen: seen,
			FirstOccurrence: a.FirstOccurrence.UTC().Format(time.RFC3339),
			LastOccurrence:  a.LastOccurrence.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

func markSeenForUID(ctx context.Context, uid uint32, ids []string) (apischema.AlertList, error) {
	if err := store.MarkSeen(ctx, uidKey(uid), ids); err != nil {
		return apischema.AlertList{}, err
	}
	return listForUID(uid)
}

func dismissForUID(ctx context.Context, uid uint32, id string) (apischema.AlertList, error) {
	if id == "" {
		return apischema.AlertList{}, bridgeipc.ErrInvalidArgs
	}
	if err := store.Dismiss(ctx, id); err != nil {
		// No bridgeipc.ErrNotFound exists; report unknown ids as invalid arguments.
		return apischema.AlertList{}, fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
	}
	return listForUID(uid)
}

func notificationsFromRequest(r apischema.NotificationSettings) store.NotificationSettings {
	return store.NotificationSettings{
		MinSeverity: r.MinSeverity, WebhookURL: r.WebhookURL,
		SMTPHost: r.SMTPHost, SMTPPort: r.SMTPPort, SMTPUsername: r.SMTPUsername, SMTPPassword: r.SMTPPassword,
		EmailFrom: r.EmailFrom, EmailTo: r.EmailTo,
	}
}

// notificationsView is the response form: the SMTP password is never sent
// back, only whether one is stored.
func notificationsView(n store.NotificationSettings) apischema.NotificationSettingsView {
	return apischema.NotificationSettingsView{
		MinSeverity: n.MinSeverity, WebhookURL: n.WebhookURL,
		SMTPHost: n.SMTPHost, SMTPPort: n.SMTPPort, SMTPUsername: n.SMTPUsername, SMTPPasswordSet: n.SMTPPassword != "",
		EmailFrom: n.EmailFrom, EmailTo: n.EmailTo,
	}
}

// handleTestNotifications sends a test alert through the saved channels,
// whatever their minimum severity, and reports the first failure.
func handleTestNotifications(ctx context.Context, _ apischema.NoRequest) (apischema.SuccessResponse, error) {
	n, err := store.LoadNotifications()
	if err != nil {
		return apischema.SuccessResponse{}, err
	}
	if n.WebhookURL == "" && n.SMTPHost == "" {
		return apischema.SuccessResponse{}, errors.New("no notification channel is configured")
	}
	if err := store.Send(ctx, n, store.TestAlert()); err != nil {
		slog.Warn("test notification failed", "error", err)
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}
