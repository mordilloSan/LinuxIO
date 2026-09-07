package daemon

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	httpapi "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/api/http"
	apimodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/api/model"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/app"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/config"
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

func (e *commandExecutor) ExecuteCommand(ctx context.Context, req apimodel.CommandRequest) httpapi.CommandResult {
	req.Command = normalizeCommand(req.Command)
	if req.RequestID == "" {
		// Give every command a correlation ID even when the caller omits one.
		req.RequestID = rand.Text()
	}
	if req.Command == "" {
		return commandError(req, http.StatusBadRequest, "invalid_command", "command is required")
	}
	switch req.Command {
	case "commands.list":
		return commandOK(req, commandList(), false)
	case "status.get":
		return commandOK(req, e.app.StatusMeta(), false)
	case "config.get":
		cfg, _, err := config.Load(e.configPath)
		if err != nil {
			return commandError(req, http.StatusInternalServerError, "config_load_failed", err.Error())
		}
		return commandOK(req, cfg.View(), false)
	case "config.set":
		return e.handleConfigSet(ctx, req)
	case "config.reload":
		return e.handleConfigReload(req)
	case "smart.refresh":
		if err := e.app.RefreshSmartNow(ctx); err != nil {
			return commandError(req, http.StatusInternalServerError, "smart_refresh_failed", err.Error())
		}
		return commandOK(req, map[string]any{"refreshed": true}, false)
	case "db.check":
		if err := e.app.CheckDatabase(); err != nil {
			return commandError(req, http.StatusInternalServerError, "db_check_failed", err.Error())
		}
		return commandOK(req, map[string]any{"path": e.app.StatusMeta().DBPath}, false)
	case "db.maintain":
		if err := e.app.MaintainDatabase(ctx); err != nil {
			return commandError(req, http.StatusInternalServerError, "db_maintain_failed", err.Error())
		}
		return commandOK(req, map[string]any{"path": e.app.StatusMeta().DBPath}, false)
	default:
		return commandError(req, http.StatusNotFound, "unknown_command", "unknown command")
	}
}

func normalizeCommand(command string) string {
	return strings.ToLower(strings.TrimSpace(command))
}

func commandList() []string {
	return []string{
		"commands.list",
		"status.get",
		"config.get",
		"config.set",
		"config.reload",
		"smart.refresh",
		"db.check",
		"db.maintain",
	}
}

func (e *commandExecutor) handleConfigReload(req apimodel.CommandRequest) httpapi.CommandResult {
	e.configMu.Lock()
	defer e.configMu.Unlock()
	cfg, _, err := config.Load(e.configPath)
	if err != nil {
		return commandError(req, http.StatusBadRequest, "invalid_config", err.Error())
	}
	return e.reloadRuntimeConfig(req, cfg, "loaded")
}

type configSetParams struct {
	CollectorInterval    *string            `json:"collector_interval"`
	SmartRefreshInterval *string            `json:"smart_refresh_interval"`
	DiskUsageCache       *string            `json:"disk_usage_cache"`
	HistoryRetention     *string            `json:"history_retention"`
	History              *string            `json:"history"`
	HistoryIntervals     *map[string]string `json:"history_intervals"` // replaces the whole map
	Listeners            *[]config.Listener `json:"listeners"`
}

func (e *commandExecutor) handleConfigSet(_ context.Context, req apimodel.CommandRequest) httpapi.CommandResult {
	if len(req.Params) == 0 {
		return commandError(req, http.StatusBadRequest, "invalid_params", "params are required")
	}
	var params configSetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
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

func applyConfigSetParams(cfg *config.Config, params configSetParams) (bool, error) {
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
		cfg.Listeners = append([]config.Listener(nil), (*params.Listeners)...)
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

func (e *commandExecutor) reloadRuntimeConfig(req apimodel.CommandRequest, cfg config.Config, source string) httpapi.CommandResult {
	if err := e.app.ReloadRuntime(runOptions(cfg, source)); err != nil {
		return commandError(req, http.StatusBadRequest, "reload_failed", err.Error())
	}
	return commandOK(req, e.app.StatusMeta(), listenersRestartRequired(e.app.Listeners(), cfg))
}

// listenersRestartRequired reports whether the configured listeners differ from
// the running ones. The two fixed sockets are never configurable, so they are
// skipped before comparing.
func listenersRestartRequired(active []apimodel.ListenerMeta, cfg config.Config) bool {
	configurable := make([]apimodel.ListenerMeta, 0, len(active))
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
			configurable[i].Address != app.GetAddress(listener.Address) ||
			!slices.EqualFunc(configurable[i].Plugins, listener.Plugins, func(active, configured string) bool {
				return strings.EqualFold(strings.TrimSpace(active), strings.TrimSpace(configured))
			}) {
			return true
		}
	}
	return false
}

func commandOK(req apimodel.CommandRequest, data any, restartRequired bool) httpapi.CommandResult {
	return httpapi.CommandResult{
		Status: http.StatusOK,
		Response: apimodel.CommandResponse{
			OK:              true,
			Command:         req.Command,
			RequestID:       req.RequestID,
			RestartRequired: restartRequired,
			Data:            data,
		},
	}
}

func commandError(req apimodel.CommandRequest, status int, code, message string) httpapi.CommandResult {
	if status == 0 {
		status = http.StatusInternalServerError
	}
	if message == "" {
		message = code
	}
	return httpapi.CommandResult{
		Status: status,
		Response: apimodel.CommandResponse{
			OK:        false,
			Command:   req.Command,
			RequestID: req.RequestID,
			Error: &apimodel.CommandError{
				Code:    code,
				Message: message,
			},
		},
	}
}
