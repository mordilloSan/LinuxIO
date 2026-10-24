package api

import (
	"encoding/json/jsontext"
)

// Command names accepted by POST RouteCommand on control.sock.
const (
	CommandList         = "commands.list"
	CommandStatusGet    = "status.get"
	CommandConfigGet    = "config.get"
	CommandConfigSet    = "config.set"
	CommandConfigReload = "config.reload"
	CommandSmartRefresh = "smart.refresh"
	CommandDBCheck      = "db.check"
	CommandDBMaintain   = "db.maintain"
)

// Commands lists every command the daemon executes, in the order it reports them.
func Commands() []string {
	return []string{
		CommandList,
		CommandStatusGet,
		CommandConfigGet,
		CommandConfigSet,
		CommandConfigReload,
		CommandSmartRefresh,
		CommandDBCheck,
		CommandDBMaintain,
	}
}

type CommandRequest struct {
	Command   string         `json:"command"`
	Params    jsontext.Value `json:"params,omitzero"`
	RequestID string         `json:"request_id,omitempty"`
}

// CommandResponse is the command envelope. The daemon fills Data with typed
// payloads (T = any); clients keep it raw (T = jsontext.Value) and decode it
// per command.
type CommandResponse[T any] struct {
	OK              bool          `json:"ok"`
	Command         string        `json:"command"`
	RequestID       string        `json:"request_id,omitempty"`
	RestartRequired bool          `json:"restart_required,omitzero"`
	Data            T             `json:"data,omitzero"`
	Error           *CommandError `json:"error,omitzero"`
}

type CommandError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse is the body of a failed non-command request.
type ErrorResponse struct {
	Error string `json:"error"`
}

// The Monitoring-prefixed types are the payloads that reach the frontend
// unchanged; bridge/apischema aliases them, and the generator names the
// TypeScript interfaces from these Go names.

type MonitoringListener struct {
	Address string   `json:"address"`
	Name    string   `json:"name"`
	Plugins []string `json:"plugins,omitempty"`
}

// MonitoringConfig is the config.get result and the config.set outcome.
type MonitoringConfig struct {
	CollectorInterval    string               `json:"collector_interval"`
	DiskUsageCache       string               `json:"disk_usage_cache"`
	History              string               `json:"history"`
	HistoryIntervals     map[string]string    `json:"history_intervals"`
	HistoryRetention     string               `json:"history_retention"`
	Listeners            []MonitoringListener `json:"listeners"`
	SmartRefreshInterval string               `json:"smart_refresh_interval"`
	Version              int                  `json:"version"`
}

// MonitoringConfigPatch is the config.set parameter object. Listeners require
// a daemon restart to take effect.
type MonitoringConfigPatch struct {
	CollectorInterval    *string               `json:"collector_interval,omitzero"`
	DiskUsageCache       *string               `json:"disk_usage_cache,omitzero"`
	History              *string               `json:"history,omitzero"`
	HistoryIntervals     *map[string]string    `json:"history_intervals,omitzero"` // replaces the whole map
	HistoryRetention     *string               `json:"history_retention,omitzero"`
	Listeners            *[]MonitoringListener `json:"listeners,omitzero"`
	SmartRefreshInterval *string               `json:"smart_refresh_interval,omitzero"`
}

type MonitoringConfigMeta struct {
	CollectorInterval string            `json:"collector_interval"`
	HistoryRetention  string            `json:"history_retention"`
	HistoryPlugins    []string          `json:"history_plugins"`
	HistoryIntervals  map[string]string `json:"history_intervals"`
	Path              string            `json:"path"`
	Source            string            `json:"source"`
	Version           int               `json:"version"`
}

type MonitoringSmartRefreshResult struct {
	Refreshed bool `json:"refreshed"`
}

type MonitoringDatabaseResult struct {
	Path string `json:"path"`
}

// ListenerMeta describes one running listener.
type ListenerMeta struct {
	Name             string   `json:"name"`
	Address          string   `json:"address"`
	EffectiveAddress string   `json:"effective_address"`
	APIs             []string `json:"apis"`
	Plugins          []string `json:"plugins,omitempty"`
	Active           bool     `json:"active"`
}

// Status is the status.get result and the RouteMeta body.
type Status struct {
	Version              string               `json:"version"`
	DataDir              string               `json:"data_dir"`
	DBPath               string               `json:"db_path"`
	DBSizeBytes          int64                `json:"db_size_bytes"` // metrics.db plus its -wal and -shm sidecars
	Listeners            []ListenerMeta       `json:"listeners,omitempty"`
	CollectorInterval    string               `json:"collector_interval"`
	SmartRefreshInterval string               `json:"smart_refresh_interval"`
	Config               MonitoringConfigMeta `json:"config"`
	Retention            map[string]string    `json:"retention"`
}
