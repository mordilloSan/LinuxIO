package daemon

import (
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/app"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/config"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/httpapi"
)

type commandExecutor struct {
	app        *app.App
	configPath string
	// configMu serialises load, patch, save and reload so concurrent
	// config.set or config.reload calls cannot drop each other's fields or
	// leave the running config behind the saved file.
	configMu sync.Mutex
}

// NewCommandExecutor returns the executor served on the control socket.
func NewCommandExecutor(a *app.App, configPath string) *commandExecutor {
	return &commandExecutor{app: a, configPath: configPath}
}

// reloadFromFile re-reads the config file and applies it to the running
// daemon. SIGHUP uses it so a reload never interleaves with config.set.
func (e *commandExecutor) reloadFromFile() error {
	e.configMu.Lock()
	defer e.configMu.Unlock()
	cfg, _, err := config.Load(e.configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	return e.app.ReloadRuntime(runOptions(cfg, "loaded"))
}

func (e *commandExecutor) ExecuteCommand(ctx context.Context, req monitoringapi.CommandRequest) httpapi.CommandResult {
	req.Command = normalizeCommand(req.Command)
	if req.RequestID == "" {
		// Give every command a correlation ID even when the caller omits one.
		req.RequestID = rand.Text()
	}
	if req.Command == "" {
		return commandError(req, http.StatusBadRequest, "invalid_command", "command is required")
	}
	switch req.Command {
	case monitoringapi.CommandList:
		return commandOK(req, monitoringapi.Commands(), false)
	case monitoringapi.CommandStatusGet:
		return commandOK(req, e.app.StatusMeta(), false)
	case monitoringapi.CommandConfigGet:
		cfg, _, err := config.Load(e.configPath)
		if err != nil {
			return commandError(req, http.StatusInternalServerError, "config_load_failed", err.Error())
		}
		return commandOK(req, cfg.View(), false)
	case monitoringapi.CommandConfigSet:
		return e.handleConfigSet(ctx, req)
	case monitoringapi.CommandConfigReload:
		return e.handleConfigReload(req)
	case monitoringapi.CommandSmartRefresh:
		if err := e.app.RefreshSmartNow(ctx); err != nil {
			return commandError(req, http.StatusInternalServerError, "smart_refresh_failed", err.Error())
		}
		return commandOK(req, monitoringapi.MonitoringSmartRefreshResult{Refreshed: true}, false)
	case monitoringapi.CommandDBCheck:
		if err := e.app.CheckDatabase(); err != nil {
			return commandError(req, http.StatusInternalServerError, "db_check_failed", err.Error())
		}
		return commandOK(req, monitoringapi.MonitoringDatabaseResult{Path: e.app.StatusMeta().DBPath}, false)
	case monitoringapi.CommandDBMaintain:
		if err := e.app.MaintainDatabase(ctx); err != nil {
			return commandError(req, http.StatusInternalServerError, "db_maintain_failed", err.Error())
		}
		return commandOK(req, monitoringapi.MonitoringDatabaseResult{Path: e.app.StatusMeta().DBPath}, false)
	default:
		return commandError(req, http.StatusNotFound, "unknown_command", "unknown command")
	}
}

func normalizeCommand(command string) string {
	return strings.ToLower(strings.TrimSpace(command))
}

func (e *commandExecutor) handleConfigReload(req monitoringapi.CommandRequest) httpapi.CommandResult {
	e.configMu.Lock()
	defer e.configMu.Unlock()
	cfg, _, err := config.Load(e.configPath)
	if err != nil {
		return commandError(req, http.StatusBadRequest, "invalid_config", err.Error())
	}
	return e.reloadRuntimeConfig(req, cfg, "loaded")
}

func (e *commandExecutor) handleConfigSet(_ context.Context, req monitoringapi.CommandRequest) httpapi.CommandResult {
	if len(req.Params) == 0 {
		return commandError(req, http.StatusBadRequest, "invalid_params", "params are required")
	}
	var params monitoringapi.MonitoringConfigPatch
	if err := json.Unmarshal(req.Params, &params, json.RejectUnknownMembers(true)); err != nil {
		return commandError(req, http.StatusBadRequest, "invalid_params", err.Error())
	}
	e.configMu.Lock()
	defer e.configMu.Unlock()
	cfg, _, err := config.Load(e.configPath)
	if err != nil {
		return commandError(req, http.StatusInternalServerError, "config_load_failed", err.Error())
	}
	restartRequired, err := applyConfigSetParams(&cfg, params)
	if err != nil {
		return commandError(req, http.StatusBadRequest, "invalid_config", err.Error())
	}
	if err := config.Save(e.configPath, cfg); err != nil {
		return commandError(req, http.StatusInternalServerError, "config_save_failed", err.Error())
	}
	reloadResult := e.reloadRuntimeConfig(req, cfg, "loaded")
	if !reloadResult.Response.OK {
		return reloadResult
	}
	reloadResult.Response.RestartRequired = reloadResult.Response.RestartRequired || restartRequired
	reloadResult.Response.Data = cfg.View()
	return reloadResult
}

func applyConfigSetParams(cfg *config.Config, params monitoringapi.MonitoringConfigPatch) (bool, error) {
	restartRequired := false
	if err := errors.Join(
		setDuration(&cfg.Collector.Interval, params.CollectorInterval),
		setDuration(&cfg.Collector.SmartRefreshInterval, params.SmartRefreshInterval),
		setDuration(&cfg.Collector.DiskUsageCache, params.DiskUsageCache),
		setDuration(&cfg.History.Retention, params.HistoryRetention),
	); err != nil {
		return false, err
	}
	if params.History != nil {
		plugins := []string{}
		for part := range strings.SplitSeq(*params.History, ",") {
			if plugin := strings.TrimSpace(part); plugin != "" {
				plugins = append(plugins, plugin)
			}
		}
		cfg.History.Plugins = plugins
	}
	if params.HistoryIntervals != nil {
		intervals := make(map[string]config.Duration, len(*params.HistoryIntervals))
		for plugin, raw := range *params.HistoryIntervals {
			var interval config.Duration
			if err := setDuration(&interval, &raw); err != nil {
				return false, fmt.Errorf("history.intervals.%s: %w", plugin, err)
			}
			intervals[strings.ToLower(strings.TrimSpace(plugin))] = interval
		}
		cfg.History.Intervals = intervals
	}
	if params.Listeners != nil {
		cfg.Listeners = nil
		for _, listener := range *params.Listeners {
			cfg.Listeners = append(cfg.Listeners, config.Listener{Name: listener.Name, Address: listener.Address, Plugins: listener.Plugins})
		}
		restartRequired = true
	}
	if err := config.Validate(*cfg); err != nil {
		return false, err
	}
	return restartRequired, nil
}

// setDuration parses an optional duration parameter in place; a nil parameter
// leaves the configured value untouched.
func setDuration(target *config.Duration, raw *string) error {
	if raw == nil {
		return nil
	}
	parsed, err := time.ParseDuration(*raw)
	if err != nil {
		return err
	}
	*target = config.Duration(parsed)
	return nil
}

func (e *commandExecutor) reloadRuntimeConfig(req monitoringapi.CommandRequest, cfg config.Config, source string) httpapi.CommandResult {
	if err := e.app.ReloadRuntime(runOptions(cfg, source)); err != nil {
		return commandError(req, http.StatusBadRequest, "reload_failed", err.Error())
	}
	return commandOK(req, e.app.StatusMeta(), listenersRestartRequired(e.app.Listeners(), cfg))
}

// listenersRestartRequired reports whether the configured listeners differ from
// the running ones. The two fixed sockets are never configurable, so they are
// skipped before comparing.
func listenersRestartRequired(active []monitoringapi.ListenerMeta, cfg config.Config) bool {
	configurable := make([]monitoringapi.ListenerMeta, 0, len(active))
	for _, listener := range active {
		if listener.Name == "api" || listener.Name == "control" {
			continue
		}
		configurable = append(configurable, listener)
	}
	if len(configurable) != len(cfg.Listeners) {
		return true
	}
	for i, listener := range cfg.Listeners {
		if configurable[i].Name != listener.Name ||
			configurable[i].Address != config.GetAddress(listener.Address) ||
			!slices.EqualFunc(configurable[i].Plugins, listener.Plugins, func(active, configured string) bool {
				return strings.EqualFold(strings.TrimSpace(active), strings.TrimSpace(configured))
			}) {
			return true
		}
	}
	return false
}

func commandOK(req monitoringapi.CommandRequest, data any, restartRequired bool) httpapi.CommandResult {
	return httpapi.CommandResult{
		Status: http.StatusOK,
		Response: monitoringapi.CommandResponse[any]{
			OK:              true,
			Command:         req.Command,
			RequestID:       req.RequestID,
			RestartRequired: restartRequired,
			Data:            data,
		},
	}
}

func commandError(req monitoringapi.CommandRequest, status int, code, message string) httpapi.CommandResult {
	if status == 0 {
		status = http.StatusInternalServerError
	}
	if message == "" {
		message = code
	}
	return httpapi.CommandResult{
		Status: status,
		Response: monitoringapi.CommandResponse[any]{
			OK:        false,
			Command:   req.Command,
			RequestID: req.RequestID,
			Error: &monitoringapi.CommandError{
				Code:    code,
				Message: message,
			},
		},
	}
}
