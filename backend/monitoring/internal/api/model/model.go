// Package model contains the JSON response contracts served by the HTTP API.
package model

import (
	"encoding/json"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/smart"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

type SummaryResponse struct {
	CapturedAt int64 `json:"captured_at"`
	system.CombinedData
}

type SystemSummaryResponse struct {
	CapturedAt int64 `json:"captured_at"`
	system.Summary
}

type HistoryItem[T any] struct {
	CapturedAt int64 `json:"captured_at"`
	Stats      T     `json:"stats"`
}

type HistoryResponse[T any] struct {
	Resolution string           `json:"resolution"`
	Items      []HistoryItem[T] `json:"items"`
}

type CurrentItemsResponse[T any] struct {
	CapturedAt int64 `json:"captured_at"`
	Items      []T   `json:"items"`
}

type CurrentDataResponse[T any] struct {
	CapturedAt int64 `json:"captured_at"`
	Data       T     `json:"data"`
}

type ContainerCurrent struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Image     string                 `json:"image,omitempty"`
	Ports     string                 `json:"ports,omitempty"`
	Status    string                 `json:"status,omitempty"`
	Health    container.DockerHealth `json:"health,omitempty"`
	Cpu       float64                `json:"cpu_percent"`
	Mem       float64                `json:"memory_mb"`
	Bandwidth [2]uint64              `json:"bandwidth_bytes,omitempty,omitzero"`
}

type SmartDeviceCurrent struct {
	ID   string          `json:"id"`
	Key  string          `json:"key"`
	Data smart.SmartData `json:"data"`
}

type MetaResponse struct {
	Version              string            `json:"version"`
	DataDir              string            `json:"data_dir"`
	DBPath               string            `json:"db_path"`
	DBSizeBytes          int64             `json:"db_size_bytes"` // metrics.db plus its -wal and -shm sidecars
	Listeners            []ListenerMeta    `json:"listeners,omitempty"`
	CollectorInterval    string            `json:"collector_interval"`
	SmartRefreshInterval string            `json:"smart_refresh_interval"`
	Config               ConfigMeta        `json:"config"`
	Retention            map[string]string `json:"retention"`
}

type ListenerMeta struct {
	Name             string   `json:"name"`
	Address          string   `json:"address"`
	EffectiveAddress string   `json:"effective_address"`
	APIs             []string `json:"apis"`
	Plugins          []string `json:"plugins,omitempty"`
	Active           bool     `json:"active"`
}

type ConfigMeta struct {
	Path              string            `json:"path"`
	Source            string            `json:"source"`
	Version           int               `json:"version"`
	CollectorInterval string            `json:"collector_interval"`
	HistoryRetention  string            `json:"history_retention"`
	HistoryPlugins    []string          `json:"history_plugins"`
	HistoryIntervals  map[string]string `json:"history_intervals"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type CommandRequest struct {
	Command   string          `json:"command"`
	Params    json.RawMessage `json:"params,omitempty"`
	RequestID string          `json:"request_id,omitempty"`
}

type CommandResponse struct {
	OK              bool          `json:"ok"`
	Command         string        `json:"command"`
	RequestID       string        `json:"request_id,omitempty"`
	RestartRequired bool          `json:"restart_required,omitempty"`
	Data            any           `json:"data,omitempty"`
	Error           *CommandError `json:"error,omitempty"`
}

type CommandError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
