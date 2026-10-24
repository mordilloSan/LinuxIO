// Package app composes the collectors into the monitoring daemon: it gathers a
// sample on every tick, persists it, answers live requests and serves the HTTP
// API. The collectors themselves live under internal/collector and the live
// payload under internal/live.
package app

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector/containertelemetry"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector/disk"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector/docker"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector/gpu"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector/host"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector/network"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector/process"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector/sensors"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector/smart"
	containermodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/httpapi"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/live"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/store"
)

// App holds the collectors and the runtime state around them. The embedded
// mutex serialises collection: the collector pass and every live collection
// hold it while they read or rewrite collector state.
type App struct {
	sync.Mutex
	host               *host.Manager    // Host info, details and the ZFS capability
	fsManager          *disk.Manager    // Filesystem and disk I/O state
	networkManager     *network.Manager // Network interface and bandwidth state
	dockerManager      *docker.Manager  // Docker API requests
	sensorConfig       *sensors.Config  // Sensors config
	gpuManager         *gpu.Manager     // GPU data
	smartManager       *smart.Manager   // SMART data
	processManager     *process.Manager // Persisted per-process CPU and I/O state
	liveProcessManager *process.Manager // Independent live API process state
	dataDir            string           // Directory for persisting data
	requestLogging     bool             // Whether HTTP API requests are logged
	store              *store.Store     // Persistent local store
	httpRuntimes       []*httpRuntime   // HTTP servers + effective listen addresses (nil before Start); guarded by runtimeMu
	reloadMu           sync.Mutex       // Serializes runtime reload application and collector notification
	runtimeMu          sync.RWMutex     // Protects mutable runtime config visible to API/reload
	intervalUpdates    chan time.Duration
	collectorInterval  time.Duration
	configPath         string
	configSource       string
	configVersion      int
	historyPlugins     []string
	historyIntervals   map[string]time.Duration // per-plugin history sampling interval; absent means collectorInterval
	historyRetention   time.Duration
	lastCollectedMs    atomic.Int64 // Unix milliseconds of the last persisted collector sample; 0 before the first tick

	telemetryMu sync.RWMutex               // Protects the container telemetry memo
	telemetry   []containermodel.Telemetry // Container telemetry from the last collector tick
	telemetryAt time.Time                  // Capture time of that telemetry; zero before the first tick

	collectorRuns   collector.Flight[struct{}, *collectorAPISample]       // The collector pass in flight, for handoff
	liveRuns        collector.Flight[uint16, liveSample]                  // Newest live collection per sample key
	liveProcessRuns collector.Flight[struct{}, map[string]jsontext.Value] // Newest live process collection
	liveFilesystems live.Filesystems
	liveSmartPower  live.SmartPower
	// liveProcessCollect overrides process collection in focused tests.
	liveProcessCollect func(context.Context) (map[string]jsontext.Value, error)
	// collectLive overrides the live collection; nil in production, injected by tests.
	collectLive func(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*systemmodel.CombinedData, error)
}

// New creates a new app that persists its data under dataDir, creating the
// directory when it does not exist.
func New(ctx context.Context, dataDir string) (*App, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	slog.Info("starting linuxio-monitoring", "version", version.Version)

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	slog.Info("Data directory", "path", dataDir)

	app := &App{
		dataDir:            dataDir,
		host:               host.NewManager(),
		fsManager:          disk.NewManager(),
		networkManager:     network.NewManager(),
		processManager:     process.NewManager(),
		liveProcessManager: process.NewManager(),
		requestLogging:     httpapi.RequestLoggingEnabled(),
		sensorConfig:       sensors.NewConfig(),
	}

	// initialize docker manager
	app.dockerManager = docker.NewManager(ctx, func() {
		app.host.UpdateDetails(func(details *systemmodel.Details) {
			details.Podman = true
		})
	})
	if app.dockerManager != nil {
		app.dockerManager.SetCollectorKey(collector.TickKey)
	}

	// initialize system info
	app.host.Refresh(ctx, app.dockerManager)

	// initialize disk info
	app.fsManager.Initialize(ctx)

	// initialize net io stats
	app.networkManager.Initialize(ctx)

	host.InitializeCPUMetrics(ctx)

	var err error
	app.smartManager, err = smart.NewManager()
	if err != nil {
		slog.Debug("SMART", "err", err)
	}

	// initialize GPU manager
	app.gpuManager, err = gpu.NewManager()
	if err != nil {
		slog.Debug("GPU", "err", err)
	}

	return app, nil
}

// DataRequestOptions controls how a stats collection request is scoped.
type DataRequestOptions struct {
	CacheTimeMs    uint16
	IncludeDetails bool
}

func (a *App) gatherStats(ctx context.Context, options DataRequestOptions) (*systemmodel.CombinedData, error) {
	data, identities, err := a.gatherBaseStats(ctx, options)
	if err != nil {
		return nil, err
	}
	if err := a.attachDefaultIntervalStats(ctx, options.CacheTimeMs, data, identities); err != nil {
		return nil, err
	}
	return data, nil
}

func (a *App) gatherBaseStats(ctx context.Context, options DataRequestOptions) (*systemmodel.CombinedData, []containermodel.Identity, error) {
	a.Lock()
	defer a.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	cacheTimeMs := options.CacheTimeMs
	data := &systemmodel.CombinedData{}
	stats, err := a.getSystemStats(ctx, cacheTimeMs)
	if err != nil {
		return nil, nil, err
	}
	*data = systemmodel.CombinedData{
		Stats: stats,
		Info:  a.host.Info,
	}

	// slog.Info("System data", "data", data, "cacheTimeMs", cacheTimeMs)

	if err := a.attachContainerStats(ctx, cacheTimeMs, data); err != nil {
		return nil, nil, err
	}
	identities := containerIdentitiesFromStats(data.Containers)
	a.attachFilesystemStats(data)
	slog.Debug("Extra FS", "count", len(data.Stats.ExtraFs))

	return a.host.AttachDetails(data, cacheTimeMs, options.IncludeDetails), identities, nil
}

func (a *App) attachContainerStats(ctx context.Context, cacheTimeMs uint16, data *systemmodel.CombinedData) error {
	if a.dockerManager == nil {
		return nil
	}
	containerStats, err := a.dockerManager.GetStats(ctx, cacheTimeMs)
	if err == nil {
		data.Containers = containerStats
		slog.Debug("Containers", "count", len(data.Containers))
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	slog.Debug("Containers", "err", err)
	return nil
}

func (a *App) attachDefaultIntervalStats(ctx context.Context, cacheTimeMs uint16, data *systemmodel.CombinedData, identities []containermodel.Identity) error {
	if cacheTimeMs != collector.TickKey {
		return nil
	}
	return a.attachCurrentDetailStats(ctx, data, identities)
}

func (a *App) attachCurrentDetailStats(ctx context.Context, data *systemmodel.CombinedData, identities []containermodel.Identity) error {
	var err error
	data.ProcessCount, data.Processes, data.Programs, err = a.processManager.CollectStats(ctx, identities)
	if err != nil {
		return err
	}
	data.ContainerTelemetry = containertelemetry.Aggregate(identities, data.Processes, a.gpuProcessSamples())
	data.Connections, err = network.CollectConnectionStats(ctx)
	if err != nil {
		return err
	}
	data.IRQs, err = network.CollectIRQStats(ctx)
	return err
}

func containerIdentitiesFromStats(items []*containermodel.Stats) []containermodel.Identity {
	identities := make([]containermodel.Identity, 0, len(items))
	for _, item := range items {
		if item == nil || (item.Id == "" && item.FullID == "") {
			continue
		}
		identities = append(identities, containermodel.Identity{ID: item.Id, FullID: item.FullID, Name: item.Name})
	}
	return identities
}

func (a *App) gpuProcessSamples() []gpu.ProcessSample {
	if a.gpuManager == nil {
		return nil
	}
	return a.gpuManager.ProcessSamples()
}
