package alerts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strconv"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	store "github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

func uidKey(uid uint32) string { return strconv.FormatUint(uint64(uid), 10) }

// listForUID lists the alerts not dismissed whose level reaches the browser,
// which errors always do, and counts those uid has not seen.
func listForUID(uid uint32) (apischema.AlertList, error) {
	doc, err := store.Load()
	if err != nil {
		return apischema.AlertList{}, err
	}
	settings := bellSettings()
	key := uidKey(uid)
	out := apischema.AlertList{Alerts: []apischema.Alert{}}
	for _, a := range doc.Alerts {
		if !shownInBell(settings, a) {
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

// bellSettings are the settings the list follows. Settings that do not parse
// list every level: hiding alerts over a broken file would hide failures too.
// Debug: the bell polls this, and alerts.get_notifications warns.
func bellSettings() store.NotificationSettings {
	settings, err := store.LoadNotifications()
	if err != nil {
		slog.Debug("notification settings unreadable, listing every level", "error", err)
		return store.DefaultNotifications()
	}
	return settings
}

// shownInBell reports whether the list shows a: not dismissed, and its level
// reaches the browser.
func shownInBell(settings store.NotificationSettings, a store.Alert) bool {
	return a.DismissedAt == nil && settings.ChannelsFor(a.Severity).Browser
}

// markSeenForUID marks ids seen for uid or, when ids is nil, every alert, but
// only those the list shows, chosen under the store's lock: one its level
// keeps out of the browser stays unseen, to count once the level is shown
// again, whether or not ids names it.
func markSeenForUID(ctx context.Context, uid uint32, ids []string) (apischema.AlertList, error) {
	settings := bellSettings()
	if err := store.MarkSeen(ctx, uidKey(uid), ids, func(a store.Alert) bool { return shownInBell(settings, a) }); err != nil {
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

// saveNotifications stores a save from the settings page under the settings
// lock, so a schedule's override written meanwhile is not lost. The page sets
// every setting it shows, so it may save over a file that does not parse.
// Settings that validation refuses came from the request: a 400.
func saveNotifications(ctx context.Context, r apischema.NotificationSettings) (store.NotificationSettings, error) {
	n, err := store.RewriteNotifications(ctx, func(n *store.NotificationSettings) error {
		*n = notificationsFromRequest(r, *n)
		return nil
	})
	if errors.Is(err, store.ErrInvalidSettings) {
		return store.NotificationSettings{}, fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
	}
	return n, err
}

// notificationsFromRequest is r over the stored settings. The script overrides
// travel with their schedules, so the stored ones are kept. Sent levels and
// report decide alone: a level or kind they leave out takes its default, and
// the request's minSeverity and dockerUpdates, which this page echoes, are
// ignored, so a value a hand edit left cannot refuse the save; the store
// derives them for an older build. A request without them comes from an older
// page, which says what it wants through minSeverity and dockerUpdates: the
// levels then follow minSeverity, and Docker updates follow dockerUpdates
// while the other kinds keep what is stored.
func notificationsFromRequest(r apischema.NotificationSettings, stored store.NotificationSettings) store.NotificationSettings {
	n := store.NotificationSettings{
		MinSeverity: r.MinSeverity, DockerUpdates: string(r.DockerUpdates), DockerUpdatesAvailable: r.DockerUpdatesAvailable, WebhookURL: r.WebhookURL,
		SMTPHost: r.SMTPHost, SMTPPort: r.SMTPPort, SMTPUsername: r.SMTPUsername, SMTPPassword: r.SMTPPassword,
		EmailFrom: r.EmailFrom, EmailTo: r.EmailTo,
		Scripts: stored.Scripts,
	}
	defaults := store.DefaultNotifications()
	if r.Levels != nil {
		n.MinSeverity = ""
		n.Levels = defaults.Levels
		for severity, c := range r.Levels {
			n.Levels[severity] = store.Channels(c)
		}
	}
	if r.Report != nil {
		n.DockerUpdates = ""
		n.Report = defaults.Report
		maps.Copy(n.Report, r.Report)
	} else {
		n.Report = map[string]string{}
		maps.Copy(n.Report, stored.Report)
		delete(n.Report, store.KindDockerUpdates)
		// Without an entry, ReportFor reads dockerUpdates, as for an older file.
		n.Report[store.KindDockerUpdates] = n.ReportFor(store.KindDockerUpdates)
	}
	return n
}

// getNotifications is the settings page's view of the stored settings. Over a
// file that does not parse it is the defaults, marked unreadable, so the page
// can show the form and save over the file.
func getNotifications() (apischema.NotificationSettingsView, error) {
	n, err := store.LoadNotifications()
	if errors.Is(err, store.ErrUnparsableSettings) {
		slog.Warn("notification settings unreadable, showing the defaults", "error", err)
		view := notificationsView(store.DefaultNotifications())
		view.SettingsUnreadable = true
		return view, nil
	}
	if err != nil {
		return apischema.NotificationSettingsView{}, err
	}
	return notificationsView(n), nil
}

// notificationsView is the response form: the SMTP password is never sent
// back, only whether one is stored.
func notificationsView(n store.NotificationSettings) apischema.NotificationSettingsView {
	levels := make(map[string]apischema.NotificationChannels, len(n.Levels))
	for severity, c := range n.Levels {
		levels[severity] = apischema.NotificationChannels(c)
	}
	return apischema.NotificationSettingsView{
		MinSeverity: n.MinSeverity, DockerUpdates: apischema.DockerUpdateNotify(n.DockerUpdates), DockerUpdatesAvailable: n.DockerUpdatesAvailable, WebhookURL: n.WebhookURL,
		SMTPHost: n.SMTPHost, SMTPPort: n.SMTPPort, SMTPUsername: n.SMTPUsername, SMTPPasswordSet: n.SMTPPassword != "",
		EmailFrom: n.EmailFrom, EmailTo: n.EmailTo,
		Report: n.Report, Levels: levels,
	}
}

// handleTestNotifications sends a test alert to every saved channel, whatever
// the levels send there, and reports their failures.
func handleTestNotifications(ctx context.Context, _ apischema.NoRequest) (apischema.SuccessResponse, error) {
	n, err := store.LoadNotifications()
	if err != nil {
		return apischema.SuccessResponse{}, err
	}
	if n.WebhookURL == "" && n.SMTPHost == "" {
		return apischema.SuccessResponse{}, errors.New("no notification channel is configured")
	}
	if err := store.Send(ctx, n, store.Channels{Email: true, Webhook: true}, store.TestAlert()); err != nil {
		return apischema.SuccessResponse{}, fmt.Errorf("send test notification: %w", err)
	}
	return apischema.SuccessResponse{Success: true}, nil
}
