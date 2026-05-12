package system

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/bridge/settings"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type systemHandlers struct {
	rt runtime.Runtime
}

// RegisterHandlers registers all system handlers with the global registry
func RegisterHandlers(rt runtime.Runtime) {
	handlers := systemHandlers{rt: rt}
	rpc.Register("system", rt, []rpc.Command{
		{Name: "get_capabilities", Handler: handleGetCapabilities},
		{Name: "get_cpu_info", Handler: handleGetCPUInfo},
		{Name: "get_sensor_info", Handler: handleGetSensorInfo},
		{Name: "get_motherboard_info", Handler: handleGetMotherboardInfo},
		{Name: "get_memory_info", Handler: handleGetMemoryInfo},
		{Name: "get_host_info", Handler: handleGetHostInfo},
		{Name: "get_uptime", Handler: handleGetUptime},
		{Name: "get_fs_info", Handler: handleGetFilesystemInfo},
		{Name: "get_processes", Handler: handleGetProcesses},
		{Name: "get_services", Handler: handleGetServices},
		{Name: "get_gpu_info", Handler: handleGetGPUInfo},
		{Name: "get_updates_fast", Handler: handleGetUpdatesFast},
		{Name: "get_network_info", Handler: handleGetNetworkInfo},
		{Name: "get_disk_throughput", Handler: handleGetDiskThroughput},
		{Name: "get_system_info", Handler: handleGetSystemInfo},
		{Name: "get_pci_devices", Handler: handleGetPCIDevices},
		{Name: "get_memory_modules", Handler: handleGetMemoryModules},
		{Name: "get_health_summary", Handler: handlers.handleGetHealthSummary},
		{Name: "list_failed_login_events", Handler: handlers.handleListFailedLoginEvents, Privileged: true},
		{Name: "dismiss_unclean_shutdown", Handler: handlers.handleDismissUncleanShutdown},
		{Name: "dismiss_failed_login_alert", Handler: handlers.handleDismissFailedLoginAlert},
		{Name: "get_server_time", Handler: handleGetServerTime},
		{Name: "get_timezones", Handler: handleGetTimezones},
	})
}

func handleGetCapabilities(ctx context.Context, args []string, emit ipc.Events) error {
	return rpc.EmitResult(emit, buildCapabilitiesResponse(ctx), nil)
}

func handleGetCPUInfo(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := FetchCPUInfo()
	return rpc.EmitResult(emit, result, err)
}

func handleGetSensorInfo(ctx context.Context, args []string, emit ipc.Events) error {
	return rpc.EmitResult(emit, FetchSensorsInfo(), nil)
}

func handleGetMotherboardInfo(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := FetchBaseboardInfo()
	return rpc.EmitResult(emit, result, err)
}

func handleGetMemoryInfo(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := FetchMemoryInfo()
	return rpc.EmitResult(emit, result, err)
}

func handleGetHostInfo(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := FetchHostInfo()
	return rpc.EmitResult(emit, result, err)
}

func handleGetUptime(ctx context.Context, args []string, emit ipc.Events) error {
	uptimeSeconds, err := FetchUptimeSeconds()
	return rpc.EmitResult(emit, uptimeSeconds, err)
}

func handleGetFilesystemInfo(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := FetchFileSystemInfo(parseIncludeAllArg(args))
	return rpc.EmitResult(emit, result, err)
}

func handleGetProcesses(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := FetchProcesses()
	return rpc.EmitResult(emit, result, err)
}

func handleGetServices(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := FetchServices()
	return rpc.EmitResult(emit, result, err)
}

func handleGetGPUInfo(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := FetchGPUInfo()
	return rpc.EmitResult(emit, result, err)
}

func handleGetUpdatesFast(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetUpdatesFast()
	return rpc.EmitResult(emit, result, err)
}

func handleGetNetworkInfo(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := FetchNetworks()
	return rpc.EmitResult(emit, result, err)
}

func handleGetDiskThroughput(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := FetchDiskThroughput()
	return rpc.EmitResult(emit, result, err)
}

func handleGetSystemInfo(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := FetchSystemInfo()
	return rpc.EmitResult(emit, result, err)
}

func handleGetPCIDevices(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := FetchPCIDevices()
	return rpc.EmitResult(emit, result, err)
}

func handleGetMemoryModules(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := FetchMemoryModules()
	return rpc.EmitResult(emit, result, err)
}

func handleGetServerTime(ctx context.Context, args []string, emit ipc.Events) error {
	return rpc.EmitResult(emit, time.Now().Format(time.RFC3339), nil)
}

func handleGetTimezones(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetTimezones()
	return rpc.EmitResult(emit, result, err)
}

func (h systemHandlers) handleGetHealthSummary(ctx context.Context, args []string, emit ipc.Events) error {
	sess := h.rt.Session
	result, err := FetchSystemHealthSummary(sess.User.Username, sess.Privileged, sess.Timing.CreatedAt)
	if err == nil && result != nil {
		applyHealthDismissals(sess.User.Username, h.rt.Store, result)
	}
	return rpc.EmitResult(emit, result, err)
}

func (h systemHandlers) handleListFailedLoginEvents(ctx context.Context, args []string, emit ipc.Events) error {
	sess := h.rt.Session
	limit := parsePositiveLimitArg(args, 24, 100)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	result, err := FetchFailedLoginEvents(ctx, sess.User.Username, sess.Timing.CreatedAt, limit)
	return rpc.EmitResult(emit, result, err)
}

// applyHealthDismissals suppresses acknowledged one-shot health signals. Any
// error reading the user's settings is treated as "not dismissed" so warnings
// still surface.
func applyHealthDismissals(username string, store *settings.UserStore, summary *SystemHealthSummary) {
	if !hasDismissibleHealthSignal(summary) {
		return
	}
	cfg, _, err := settings.SnapshotForUser(username, store)
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

func applyUncleanShutdownDismissal(summary *SystemHealthSummary, dismissals *settings.Dismissals) {
	if !summary.UncleanShutdown || summary.UncleanShutdownBootID == "" {
		return
	}
	if dismissals.UncleanShutdownBootID == summary.UncleanShutdownBootID {
		summary.UncleanShutdown = false
		summary.UncleanShutdownBootID = ""
	}
}

func applyFailedLoginAlertDismissal(summary *SystemHealthSummary, dismissals *settings.Dismissals) {
	if summary.FailedLoginAlert == nil || summary.FailedLoginAlert.ID == "" {
		return
	}
	if dismissals.FailedLoginAlertID == summary.FailedLoginAlert.ID {
		summary.FailedLoginAlert = nil
	}
}

func (h systemHandlers) handleDismissUncleanShutdown(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 1); err != nil {
		return err
	}
	username := h.rt.Username()
	bootID := strings.TrimSpace(args[0])
	if !isValidBootID(bootID) {
		return ipc.ErrInvalidArgs
	}

	if _, _, err := settings.UpdateForUser(username, h.rt.Store, func(cfg *settings.Settings) error {
		if cfg.Dismissals == nil {
			cfg.Dismissals = &settings.Dismissals{}
		}
		cfg.Dismissals.UncleanShutdownBootID = bootID
		return nil
	}); err != nil {
		return fmt.Errorf("update config: %w", err)
	}
	slog.Info("dismissed unclean shutdown", "user", username, "bootId", bootID)
	return rpc.EmitResult(emit, map[string]any{"message": "dismissed"}, nil)
}

func (h systemHandlers) handleDismissFailedLoginAlert(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 1); err != nil {
		return err
	}
	username := h.rt.Username()
	alertID := strings.TrimSpace(args[0])
	if !isValidFailedLoginAlertID(alertID) {
		return ipc.ErrInvalidArgs
	}

	if _, _, err := settings.UpdateForUser(username, h.rt.Store, func(cfg *settings.Settings) error {
		if cfg.Dismissals == nil {
			cfg.Dismissals = &settings.Dismissals{}
		}
		cfg.Dismissals.FailedLoginAlertID = alertID
		return nil
	}); err != nil {
		return fmt.Errorf("update config: %w", err)
	}
	slog.Info("dismissed failed login alert", "user", username, "alertId", alertID)
	return rpc.EmitResult(emit, map[string]any{"message": "dismissed"}, nil)
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
