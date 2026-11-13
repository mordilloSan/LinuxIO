package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

// now is the server's clock; tests replace it.
var now = time.Now

// applyDismissalsUpdate applies the dismissals the payload sets. The
// history time is stored as the server writes it, in UTC, and never later
// than the server's now: a browser clock running ahead would otherwise hide
// the failures after it from the next sign-in notice.
func applyDismissalsUpdate(dismissals **userconfig.PersistedDismissals, payload *apischema.ConfigDismissalsPayload) error {
	var historySeenAt string
	if payload.HistorySeenAt != nil {
		if sent := strings.TrimSpace(*payload.HistorySeenAt); sent != "" {
			seen, err := time.Parse(time.RFC3339, sent)
			if err != nil {
				return fmt.Errorf("%w: dismissals.historySeenAt must be an RFC 3339 time: %w", bridgeipc.ErrInvalidArgs, err)
			}
			if current := now(); seen.After(current) {
				seen = current
			}
			historySeenAt = seen.UTC().Format(time.RFC3339Nano)
		}
	}
	if *dismissals == nil {
		*dismissals = &userconfig.PersistedDismissals{}
	}
	if payload.UncleanShutdownBootID != nil {
		(*dismissals).UncleanShutdownBootID = strings.TrimSpace(*payload.UncleanShutdownBootID)
	}
	if payload.FailedLoginAlertID != nil {
		(*dismissals).FailedLoginAlertID = strings.TrimSpace(*payload.FailedLoginAlertID)
	}
	if payload.HistorySeenAt != nil {
		(*dismissals).HistorySeenAt = historySeenAt
	}
	if **dismissals == (userconfig.PersistedDismissals{}) {
		*dismissals = nil
	}
	return nil
}
