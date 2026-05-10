package system

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/privilege"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

type systemRegistration struct {
	command    string
	handler    ipc.HandlerFunc
	privileged bool
}

// RegisterHandlers registers all system handlers with the global registry
func RegisterHandlers(sess *session.Session, store *config.UserStore) {
	registerCapabilitiesHandlers()
	registerSystemHandlers(sess, []systemRegistration{
		{command: "get_cpu_info", handler: handleGetCPUInfo},
		{command: "get_sensor_info", handler: handleGetSensorInfo},
		{command: "get_motherboard_info", handler: handleGetMotherboardInfo},
		{command: "get_memory_info", handler: handleGetMemoryInfo},
		{command: "get_host_info", handler: handleGetHostInfo},
		{command: "get_uptime", handler: handleGetUptime},
		{command: "get_fs_info", handler: handleGetFilesystemInfo},
		{command: "get_processes", handler: handleGetProcesses},
		{command: "get_services", handler: handleGetServices},
		{command: "get_gpu_info", handler: handleGetGPUInfo},
		{command: "get_updates_fast", handler: handleGetUpdatesFast},
		{command: "get_network_info", handler: handleGetNetworkInfo},
		{command: "get_disk_throughput", handler: handleGetDiskThroughput},
		{command: "get_system_info", handler: handleGetSystemInfo},
		{command: "get_pci_devices", handler: handleGetPCIDevices},
		{command: "get_memory_modules", handler: handleGetMemoryModules},
		{command: "get_health_summary", handler: makeGetHealthSummaryHandler(sess, store)},
		{command: "list_failed_login_events", handler: makeListFailedLoginEventsHandler(sess), privileged: true},
		{command: "dismiss_unclean_shutdown", handler: makeDismissUncleanShutdownHandler(sess, store)},
		{command: "dismiss_failed_login_alert", handler: makeDismissFailedLoginAlertHandler(sess, store)},
		{command: "get_server_time", handler: handleGetServerTime},
		{command: "get_timezones", handler: handleGetTimezones},
	})
}

func registerSystemHandlers(sess *session.Session, registrations []systemRegistration) {
	for _, registration := range registrations {
		handler := registration.handler
		if registration.privileged {
			handler = privilege.RequirePrivilegedIPC(sess, handler)
		}
		ipc.RegisterFunc("system", registration.command, handler)
	}
}

func handleGetCPUInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchCPUInfo)
}

func handleGetSensorInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emit.Result(FetchSensorsInfo())
}

func handleGetMotherboardInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchBaseboardInfo)
}

func handleGetMemoryInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchMemoryInfo)
}

func handleGetHostInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchHostInfo)
}

func handleGetUptime(ctx context.Context, args []string, emit ipc.Events) error {
	uptimeSeconds, err := FetchUptimeSeconds()
	if err != nil {
		return err
	}
	return emit.Result(uptimeSeconds)
}

func handleGetFilesystemInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemArgCall(emit, parseIncludeAllArg(args), FetchFileSystemInfo)
}

func handleGetProcesses(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchProcesses)
}

func handleGetServices(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchServices)
}

func handleGetGPUInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchGPUInfo)
}

func handleGetUpdatesFast(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, GetUpdatesFast)
}

func handleGetNetworkInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchNetworks)
}

func handleGetDiskThroughput(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchDiskThroughput)
}

func handleGetSystemInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchSystemInfo)
}

func handleGetPCIDevices(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchPCIDevices)
}

func handleGetMemoryModules(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, FetchMemoryModules)
}

func handleGetServerTime(ctx context.Context, args []string, emit ipc.Events) error {
	return emit.Result(time.Now().Format(time.RFC3339))
}

func handleGetTimezones(ctx context.Context, args []string, emit ipc.Events) error {
	return emitSystemCall(emit, GetTimezones)
}

func makeGetHealthSummaryHandler(sess *session.Session, store *config.UserStore) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := FetchSystemHealthSummary(sess.User.Username, sess.Privileged, sess.Timing.CreatedAt)
		if err == nil && result != nil {
			applyHealthDismissals(sess.User.Username, store, result)
		}
		return emitSystemResult(emit, result, err)
	}
}

func makeListFailedLoginEventsHandler(sess *session.Session) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		limit := parsePositiveLimitArg(args, 24, 100)
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		result, err := FetchFailedLoginEvents(ctx, sess.User.Username, sess.Timing.CreatedAt, limit)
		return emitSystemResult(emit, result, err)
	}
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

func makeDismissUncleanShutdownHandler(sess *session.Session, store *config.UserStore) ipc.HandlerFunc {
	username := sess.User.Username
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		bootID := strings.TrimSpace(args[0])
		if !isValidBootID(bootID) {
			return ipc.ErrInvalidArgs
		}

		if _, _, err := config.UpdateForUser(username, store, func(cfg *config.Settings) error {
			if cfg.Dismissals == nil {
				cfg.Dismissals = &config.Dismissals{}
			}
			cfg.Dismissals.UncleanShutdownBootID = bootID
			return nil
		}); err != nil {
			return fmt.Errorf("update config: %w", err)
		}
		slog.Info("dismissed unclean shutdown", "user", username, "bootId", bootID)
		return emit.Result(map[string]any{"message": "dismissed"})
	}
}

func makeDismissFailedLoginAlertHandler(sess *session.Session, store *config.UserStore) ipc.HandlerFunc {
	username := sess.User.Username
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		alertID := strings.TrimSpace(args[0])
		if !isValidFailedLoginAlertID(alertID) {
			return ipc.ErrInvalidArgs
		}

		if _, _, err := config.UpdateForUser(username, store, func(cfg *config.Settings) error {
			if cfg.Dismissals == nil {
				cfg.Dismissals = &config.Dismissals{}
			}
			cfg.Dismissals.FailedLoginAlertID = alertID
			return nil
		}); err != nil {
			return fmt.Errorf("update config: %w", err)
		}
		slog.Info("dismissed failed login alert", "user", username, "alertId", alertID)
		return emit.Result(map[string]any{"message": "dismissed"})
	}
}

// isValidBootID guards against an unbounded write to the user's settings file.
// Real boot IDs are short unix-epoch seconds strings (≤ 11 digits); allow up
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

func parseIncludeAllArg(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func parsePositiveLimitArg(args []string, fallback, max int) int {
	if fallback <= 0 {
		fallback = 24
	}
	if max <= 0 {
		max = fallback
	}
	if len(args) == 0 {
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil || value <= 0 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}

func emitSystemResult(emit ipc.Events, result any, err error) error {
	if err != nil {
		return err
	}
	return emit.Result(result)
}

func emitSystemCall[T any](emit ipc.Events, fn func() (T, error)) error {
	result, err := fn()
	return emitSystemResult(emit, result, err)
}

func emitSystemArgCall[A any, T any](emit ipc.Events, arg A, fn func(A) (T, error)) error {
	result, err := fn(arg)
	return emitSystemResult(emit, result, err)
}
