package apischema

import (
	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

type MonitoringHistoryResolution string

// MonitoringLive embeds the daemon's shared live wire contract so the bridge
// generator flattens the fields into the public route result.
type MonitoringLive struct {
	monitoringapi.Live
}

type MonitoringProcessesResponse struct {
	CapturedAtMs int64                      `json:"captured_at_ms"`
	Count        monitoringapi.ProcessCount `json:"count"`
	Items        []monitoringapi.Process    `json:"items"`
}

type MonitoringProgramsResponse struct {
	CapturedAtMs int64                   `json:"captured_at_ms"`
	Items        []monitoringapi.Program `json:"items"`
}

// The config and command-result payloads are the daemon's wire types.
type (
	MonitoringSmartRefreshResult = monitoringapi.MonitoringSmartRefreshResult
	MonitoringDatabaseResult     = monitoringapi.MonitoringDatabaseResult
	MonitoringListener           = monitoringapi.MonitoringListener
	MonitoringConfig             = monitoringapi.MonitoringConfig
	MonitoringConfigMeta         = monitoringapi.MonitoringConfigMeta
	MonitoringConfigPatch        = monitoringapi.MonitoringConfigPatch
)

type MonitoringConfigSetResult struct {
	Config          MonitoringConfig `json:"config"`
	RestartRequired bool             `json:"restart_required"`
}

type MonitoringHistoryRequest struct {
	Resolution MonitoringHistoryResolution `json:"resolution"`
	FromMs     int64                       `json:"from_ms,omitzero"`
	ToMs       int64                       `json:"to_ms,omitzero"`
	WindowMs   int64                       `json:"window_ms,omitzero"`
	Limit      int                         `json:"limit,omitzero"`
}

type MonitoringCPUHistoryPoint struct {
	CapturedAtMs int64   `json:"captured_at_ms"`
	UsagePercent float64 `json:"usage_percent"`
	// BreakdownPercent is [user, system, iowait, steal, idle].
	BreakdownPercent []float64 `json:"breakdown_percent,omitempty"`
	CoresPercent     []float64 `json:"cores_percent,omitempty"`
	// FrequenciesMHz is the per-core scaling frequency averaged over the point.
	FrequenciesMHz []float64 `json:"frequencies_mhz,omitempty"`
}

type MonitoringMemoryHistoryPoint struct {
	CapturedAtMs  int64   `json:"captured_at_ms"`
	TotalGB       float64 `json:"total_gb"`
	UsedGB        float64 `json:"used_gb"`
	UsedPercent   float64 `json:"used_percent"`
	BufferCacheGB float64 `json:"buffer_cache_gb"`
	ZFSArcGB      float64 `json:"zfs_arc_gb,omitzero"`
	// CachedGB and BuffersGB split BufferCacheGB.
	CachedGB  float64 `json:"cached_gb,omitzero"`
	BuffersGB float64 `json:"buffers_gb,omitzero"`
	// DockerUsedGB sums container memory from the containers history plugin;
	// zero when that plugin has no sample near the point's timestamp.
	DockerUsedGB float64 `json:"docker_used_gb,omitzero"`
}

type MonitoringDiskIOHistoryPoint struct {
	CapturedAtMs     int64   `json:"captured_at_ms"`
	ReadBytesPerSec  float64 `json:"read_bytes_per_sec"`
	WriteBytesPerSec float64 `json:"write_bytes_per_sec"`
}

type MonitoringNetworkRates struct {
	SentBytesPerSec float64 `json:"sent_bytes_per_sec"`
	RecvBytesPerSec float64 `json:"recv_bytes_per_sec"`
}

type MonitoringNetworkHistoryPoint struct {
	CapturedAtMs    int64                             `json:"captured_at_ms"`
	SentBytesPerSec float64                           `json:"sent_bytes_per_sec"`
	RecvBytesPerSec float64                           `json:"recv_bytes_per_sec"`
	Interfaces      map[string]MonitoringNetworkRates `json:"interfaces,omitempty"`
}

// MonitoringContainerSample is one container's slice of a container history
// point. ID is the agent's short container ID, which prefixes the full ID
// Docker inventory reports.
type MonitoringContainerSample struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	CPUPercent      float64 `json:"cpu_percent"`
	MemoryMB        float64 `json:"memory_mb"`
	SentBytesPerSec float64 `json:"sent_bytes_per_sec"`
	RecvBytesPerSec float64 `json:"recv_bytes_per_sec"`
	// ReadBytesPerSec and WriteBytesPerSec are block I/O summed over the
	// container's processes by the agent's container_telemetry plugin. Nil when
	// that plugin has no sample near this point, so the UI can say
	// "unavailable" instead of drawing a zero line.
	ReadBytesPerSec  *float64 `json:"read_bytes_per_sec,omitempty"`
	WriteBytesPerSec *float64 `json:"write_bytes_per_sec,omitempty"`
}

type MonitoringContainerHistoryPoint struct {
	CapturedAtMs int64                       `json:"captured_at_ms"`
	Containers   []MonitoringContainerSample `json:"containers"`
}

// MonitoringListenerStatus is the frontend view of the daemon's listener
// metadata; the per-listener plugin allowlist stays daemon-side.
type MonitoringListenerStatus struct {
	Active           bool     `json:"active"`
	Address          string   `json:"address"`
	APIs             []string `json:"apis"`
	EffectiveAddress string   `json:"effective_address"`
	Name             string   `json:"name"`
}

type MonitoringStatus struct {
	CollectorInterval    string                     `json:"collector_interval"`
	Config               MonitoringConfigMeta       `json:"config"`
	DataDir              string                     `json:"data_dir"`
	DBPath               string                     `json:"db_path"`
	DBSizeBytes          int64                      `json:"db_size_bytes"`
	Listeners            []MonitoringListenerStatus `json:"listeners,omitempty"`
	Retention            map[string]string          `json:"retention"`
	SmartRefreshInterval string                     `json:"smart_refresh_interval"`
	Version              string                     `json:"version"`
}
