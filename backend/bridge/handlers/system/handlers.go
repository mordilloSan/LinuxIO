package system

import (
	"context"
	"fmt"
	"log/slog"
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
func RegisterHandlers(sess *session.Session) {
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
		{command: "get_health_summary", handler: makeGetHealthSummaryHandler(sess)},
		{command: "dismiss_unclean_shutdown", handler: makeDismissUncleanShutdownHandler(sess)},
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

func makeGetHealthSummaryHandler(sess *session.Session) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := FetchSystemHealthSummary(sess.User.Username, sess.Privileged)
		if err == nil && result != nil {
			applyUncleanShutdownDismissal(sess.User.Username, result)
		}
		return emitSystemResult(emit, result, err)
	}
}

// applyUncleanShutdownDismissal suppresses the unclean-shutdown flag when the
// caller has already acknowledged the current event. Any error reading the
// user's settings is treated as "not dismissed" so the warning still surfaces.
func applyUncleanShutdownDismissal(username string, summary *SystemHealthSummary) {
	if !summary.UncleanShutdown || summary.UncleanShutdownBootID == "" {
		return
	}
	cfg, _, err := config.Load(username)
	if err != nil {
		slog.Debug("unclean-shutdown dismissal: settings unavailable, keeping warning", "user", username, "error", err)
		return
	}
	if cfg.Dismissals == nil {
		return
	}
	if cfg.Dismissals.UncleanShutdownBootID != summary.UncleanShutdownBootID {
		return
	}
	summary.UncleanShutdown = false
	summary.UncleanShutdownBootID = ""
}

func makeDismissUncleanShutdownHandler(sess *session.Session) ipc.HandlerFunc {
	username := sess.User.Username
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		bootID := strings.TrimSpace(args[0])
		if !isValidBootID(bootID) {
			return ipc.ErrInvalidArgs
		}

		cfg, _, err := config.Load(username)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if cfg.Dismissals == nil {
			cfg.Dismissals = &config.Dismissals{}
		}
		cfg.Dismissals.UncleanShutdownBootID = bootID

		if _, err := config.Save(username, cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		slog.Info("dismissed unclean shutdown", "user", username, "bootId", bootID)
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
