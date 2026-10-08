package alerts

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	store "github.com/mordilloSan/LinuxIO/backend/common/alerts"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
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
