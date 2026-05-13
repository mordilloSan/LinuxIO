package system

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func GetHealthSummaryForRuntime(ctx context.Context, rt runtime.Runtime) (*SystemHealthSummary, error) {
	session := rt.Session
	result, err := FetchSystemHealthSummary(ctx, session.User.Username, session.Privileged, session.Timing.CreatedAt)
	if err == nil && result != nil {
		applyHealthDismissals(session.User.Username, rt.Store, result)
	}
	return result, err
}

func ListFailedLoginEventsForRuntime(ctx context.Context, rt runtime.Runtime, args []string) ([]SystemLoginEvent, error) {
	session := rt.Session
	limit := parsePositiveLimitArg(args, 24, 100)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return FetchFailedLoginEvents(ctx, session.User.Username, session.Timing.CreatedAt, limit)
}

func DismissUncleanShutdownForRuntime(rt runtime.Runtime, args []string) (map[string]any, error) {
	if err := bridgeipc.RequireArgs(args, 1); err != nil {
		return nil, err
	}
	username := rt.Username()
	bootID := strings.TrimSpace(args[0])
	if !isValidBootID(bootID) {
		return nil, bridgeipc.ErrInvalidArgs
	}

	if _, _, err := config.UpdateForUser(username, rt.Store, func(cfg *config.Settings) error {
		if cfg.Dismissals == nil {
			cfg.Dismissals = &config.Dismissals{}
		}
		cfg.Dismissals.UncleanShutdownBootID = bootID
		return nil
	}); err != nil {
		return nil, fmt.Errorf("update config: %w", err)
	}
	slog.Info("dismissed unclean shutdown", "user", username, "bootId", bootID)
	return map[string]any{"message": "dismissed"}, nil
}

func DismissFailedLoginAlertForRuntime(rt runtime.Runtime, args []string) (map[string]any, error) {
	if err := bridgeipc.RequireArgs(args, 1); err != nil {
		return nil, err
	}
	username := rt.Username()
	alertID := strings.TrimSpace(args[0])
	if !isValidFailedLoginAlertID(alertID) {
		return nil, bridgeipc.ErrInvalidArgs
	}

	if _, _, err := config.UpdateForUser(username, rt.Store, func(cfg *config.Settings) error {
		if cfg.Dismissals == nil {
			cfg.Dismissals = &config.Dismissals{}
		}
		cfg.Dismissals.FailedLoginAlertID = alertID
		return nil
	}); err != nil {
		return nil, fmt.Errorf("update config: %w", err)
	}
	slog.Info("dismissed failed login alert", "user", username, "alertId", alertID)
	return map[string]any{"message": "dismissed"}, nil
}

// applyHealthDismissals suppresses acknowledged one-shot health signals. Any
// error reading the user's settings is treated as "not dismissed" so warnings
// still surface.
func applyHealthDismissals(username string, store *config.UserStore, summary *SystemHealthSummary) {
	if !hasDismissibleHealthSignal(summary) {
		return
	}
	cfg, _, err := config.SnapshotForUser(username, store)
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

func hasDismissibleHealthSignal(summary *SystemHealthSummary) bool {
	return (summary.UncleanShutdown && summary.UncleanShutdownBootID != "") ||
		(summary.FailedLoginAlert != nil && summary.FailedLoginAlert.ID != "")
}

func applyUncleanShutdownDismissal(summary *SystemHealthSummary, dismissals *config.Dismissals) {
	if !summary.UncleanShutdown || summary.UncleanShutdownBootID == "" {
		return
	}
	if dismissals.UncleanShutdownBootID == summary.UncleanShutdownBootID {
		summary.UncleanShutdown = false
		summary.UncleanShutdownBootID = ""
	}
}

func applyFailedLoginAlertDismissal(summary *SystemHealthSummary, dismissals *config.Dismissals) {
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
