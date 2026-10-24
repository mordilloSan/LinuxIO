package system

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func GetHealthSummaryForRuntime(ctx context.Context, rt runtime.Runtime) (*apischema.SystemHealthSummary, error) {
	session := rt.Session
	result, err := FetchSystemHealthSummary(ctx, session.User.Username, session.Privileged, session.Timing.CreatedAt)
	if err == nil && result != nil {
		applyHealthDismissals(ctx, session.User.Username, rt.Store, result)
	}
	return result, err
}

func ListFailedLoginEventsForRuntime(ctx context.Context, rt runtime.Runtime, req apischema.FailedLoginEventsRequest) ([]apischema.AccountUserLogin, error) {
	session := rt.Session
	limit := parsePositiveLimit(req.Limit, 24, 100)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return FetchFailedLoginEvents(ctx, session.User.Username, session.Timing.CreatedAt, limit)
}

func DismissUncleanShutdownForRuntime(ctx context.Context, rt runtime.Runtime, req apischema.BootIDRequest) (apischema.MessageResponse, error) {
	username := rt.Username()
	bootID := strings.TrimSpace(req.BootID)
	if !isValidBootID(bootID) {
		return apischema.MessageResponse{}, bridgeipc.ErrInvalidArgs
	}

	if _, _, err := userconfig.UpdateForUser(ctx, username, rt.Store, func(cfg *userconfig.Settings) error {
		if cfg.Dismissals == nil {
			cfg.Dismissals = &userconfig.PersistedDismissals{}
		}
		cfg.Dismissals.UncleanShutdownBootID = bootID
		return nil
	}); err != nil {
		return apischema.MessageResponse{}, fmt.Errorf("update config: %w", err)
	}
	slog.Info("dismissed unclean shutdown", "user", username, "bootId", bootID)
	return apischema.MessageResponse{Message: "dismissed"}, nil
}

func DismissFailedLoginAlertForRuntime(ctx context.Context, rt runtime.Runtime, req apischema.AlertIDRequest) (apischema.MessageResponse, error) {
	username := rt.Username()
	alertID := strings.TrimSpace(req.AlertID)
	if !isValidFailedLoginAlertID(alertID) {
		return apischema.MessageResponse{}, bridgeipc.ErrInvalidArgs
	}

	if _, _, err := userconfig.UpdateForUser(ctx, username, rt.Store, func(cfg *userconfig.Settings) error {
		if cfg.Dismissals == nil {
			cfg.Dismissals = &userconfig.PersistedDismissals{}
		}
		cfg.Dismissals.FailedLoginAlertID = alertID
		return nil
	}); err != nil {
		return apischema.MessageResponse{}, fmt.Errorf("update config: %w", err)
	}
	slog.Info("dismissed failed login alert", "user", username, "alertId", alertID)
	return apischema.MessageResponse{Message: "dismissed"}, nil
}

// applyHealthDismissals suppresses acknowledged one-shot health signals. Any
// error reading the user's settings is treated as "not dismissed" so warnings
// still surface.
func applyHealthDismissals(ctx context.Context, username string, store *userconfig.UserStore, summary *apischema.SystemHealthSummary) {
	if !hasDismissibleHealthSignal(summary) {
		return
	}
	cfg, _, err := userconfig.SnapshotForUser(ctx, username, store)
	if err != nil {
		slog.Debug("health dismissal: settings unavailable, keeping warnings", "user", username, "error", err)
		return
	}
	if cfg.Dismissals == nil {
		return
	}
	applyUncleanShutdownDismissal(summary, cfg.Dismissals)
	applyFailedLoginAlertDismissal(summary, cfg.Dismissals)
}

func hasDismissibleHealthSignal(summary *apischema.SystemHealthSummary) bool {
	return (summary.UncleanShutdown && apischema.StringValue(summary.UncleanShutdownBootID) != "") ||
		(summary.FailedLoginAlert != nil && summary.FailedLoginAlert.ID != "")
}

func applyUncleanShutdownDismissal(summary *apischema.SystemHealthSummary, dismissals *userconfig.PersistedDismissals) {
	bootID := apischema.StringValue(summary.UncleanShutdownBootID)
	if !summary.UncleanShutdown || bootID == "" {
		return
	}
	if dismissals.UncleanShutdownBootID == bootID {
		summary.UncleanShutdown = false
		summary.UncleanShutdownBootID = nil
	}
}

func applyFailedLoginAlertDismissal(summary *apischema.SystemHealthSummary, dismissals *userconfig.PersistedDismissals) {
	if summary.FailedLoginAlert == nil || summary.FailedLoginAlert.ID == "" {
		return
	}
	if dismissals.FailedLoginAlertID == summary.FailedLoginAlert.ID {
		summary.FailedLoginAlert = nil
	}
}

// isValidBootID guards against an unbounded write to the user's settings file.
// Real boot IDs are short unix-epoch seconds strings (<= 11 digits); allow up
// to 32 digits for headroom.
func isValidBootID(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isValidFailedLoginAlertID(s string) bool {
	const prefix = "failed_login_"
	if !strings.HasPrefix(s, prefix) || len(s) != len(prefix)+64 {
		return false
	}
	for _, r := range s[len(prefix):] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
