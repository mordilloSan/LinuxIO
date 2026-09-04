// Package app runs the standalone monitoring agent, collecting local system
// metrics, storing them in SQLite, and serving them over the built-in HTTP API.
package app

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
	httpapi "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/api/http"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	dockerintegration "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/integration/docker"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/store"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/utils"
)

// collectorDataKeyMs isolates collector delta state from live API samples. It
// identifies the collector path; the configured ticker controls its cadence.
const collectorDataKeyMs uint16 = 60_000

type App struct {
	sync.Mutex                                    // Used to lock agent while collecting data
	memCalc            string                     // Memory calculation formula
	fsManager          *fsManager                 // Manages filesystem and disk I/O state
	networkManager     *networkManager            // Manages network interface and bandwidth state
	dockerManager      *dockerintegration.Manager // Manages Docker API requests
	sensorConfig       *SensorConfig              // Sensors config
	systemInfoManager  *systemInfoManager         // Manages host info, details, and ZFS capability
	gpuManager         *GPUManager                // Manages GPU data
	dataDir            string                     // Directory for persisting data
	smartManager       *SmartManager              // Manages SMART data
	processManager     *processManager            // Manages persisted per-process CPU and I/O state
	liveProcessManager *processManager            // Manages independent live API process state
	requestLogging     bool                       // Whether HTTP API requests are logged
	store              *store.Store               // Persistent local store
	httpRuntimes       []*httpRuntime             // HTTP servers + effective listen addresses (nil before Start); guarded by runtimeMu
	collectorHandoffMu sync.Mutex                 // Protects the currently active collector handoff
	activeCollectorRun *collectorRun              // Non-nil only while a collector sample is in flight
	reloadMu           sync.Mutex                 // Serializes runtime reload application and collector notification
	runtimeMu          sync.RWMutex               // Protects mutable runtime config visible to API/reload
	intervalUpdates    chan time.Duration
	collectorInterval  time.Duration
	configPath         string
	configSource       string
	configVersion      int
	historyPlugins     []string
	historyIntervals   map[string]time.Duration // per-plugin history sampling interval; absent means collectorInterval
	historyRetention   time.Duration
	lastCollectedMs    atomic.Int64 // Unix milliseconds of the last persisted collector sample; 0 before the first tick

	telemetryMu sync.RWMutex          // Protects the container telemetry memo
	telemetry   []container.Telemetry // Container telemetry from the last collector tick
	telemetryAt time.Time             // Capture time of that telemetry; zero before the first tick

	liveMu   sync.Mutex          // Protects liveRuns
	liveRuns map[uint16]*liveRun // Newest live collection per sample key; see liveCurrentData
	// collectLive overrides the live collection; nil in production, injected by tests.
	collectLive func(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*system.CombinedData, error)
}

// New creates a new app with the given data directory for persisting data.
// If the data directory is not set, it will attempt to find the optimal directory.
func New(ctx context.Context, dataDir ...string) (app *App, err error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	app = &App{
		systemInfoManager: newSystemInfoManager(),
	}

	slog.Info("starting linuxio-monitoring", "version", version.Version)

	app.dataDir, err = store.GetDataDir(dataDir...)
	if err != nil {
		slog.Warn("Data directory not found")
	} else {
		slog.Info("Data directory", "path", app.dataDir)
	}
	app.fsManager = newFsManager()
	app.networkManager = newNetworkManager()
	app.processManager = newProcessManager()
	app.liveProcessManager = newProcessManager()
	app.memCalc, _ = utils.GetEnv("MEM_CALC")
	app.requestLogging = httpapi.RequestLoggingEnabled()
	app.sensorConfig = app.newSensorConfig()

	// initialize docker manager
	app.dockerManager = dockerintegration.NewManager(ctx, func() {
		app.systemInfoManager.updateSystemDetails(func(details *system.Details) {
			details.Podman = true
		})
	})
	if app.dockerManager != nil {
		app.dockerManager.SetCollectorKey(collectorDataKeyMs)
	}

	// initialize system info
	app.systemInfoManager.refreshSystemDetails(ctx, app.dockerManager)

	// initialize disk info
	app.fsManager.initializeDiskInfo(ctx)

	// initialize net io stats
	app.networkManager.initializeNetIoStats(ctx)

	initializeCpuMetrics(ctx)

	app.smartManager, err = NewSmartManager()
	if err != nil {
		slog.Debug("SMART", "err", err)
	}

	// initialize GPU manager
	app.gpuManager, err = NewGPUManager()
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

func (a *App) gatherStats(ctx context.Context, options DataRequestOptions) (*system.CombinedData, error) {
	data, identities, err := a.gatherBaseStats(ctx, options)
	if err != nil {
		return nil, err
	}
	if err := a.attachDefaultIntervalStats(ctx, options.CacheTimeMs, data, identities); err != nil {
		return nil, err
	}
	return data, nil
}

func (a *App) gatherBaseStats(ctx context.Context, options DataRequestOptions) (*system.CombinedData, []container.Identity, error) {
	a.Lock()
	defer a.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	cacheTimeMs := options.CacheTimeMs
	data := &system.CombinedData{}
	stats, err := a.getSystemStats(ctx, cacheTimeMs)
	if err != nil {
		return nil, nil, err
	}
	*data = system.CombinedData{
		Stats: stats,
		Info:  a.systemInfoManager.systemInfo,
	}

	// slog.Info("System data", "data", data, "cacheTimeMs", cacheTimeMs)

	if err := a.attachContainerStats(ctx, cacheTimeMs, data); err != nil {
		return nil, nil, err
	}
	identities := containerIdentitiesFromStats(data.Containers)
	a.attachFilesystemStats(data)
	slog.Debug("Extra FS", "count", len(data.Stats.ExtraFs))

	return a.systemInfoManager.attachSystemDetails(data, cacheTimeMs, options.IncludeDetails), identities, nil
}

func (a *App) attachContainerStats(ctx context.Context, cacheTimeMs uint16, data *system.CombinedData) error {
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

func (a *App) attachDefaultIntervalStats(ctx context.Context, cacheTimeMs uint16, data *system.CombinedData, identities []container.Identity) error {
	if cacheTimeMs != collectorDataKeyMs {
		return nil
	}
	return a.attachCurrentDetailStats(ctx, data, identities)
}

func (a *App) attachCurrentDetailStats(ctx context.Context, data *system.CombinedData, identities []container.Identity) error {
	var err error
	data.ProcessCount, data.Processes, data.Programs, err = a.processManager.collectProcessStats(ctx, identities)
	if err != nil {
		return err
	}
	var gpuProcesses []GPUProcessSample
	if a.gpuManager != nil {
		gpuProcesses = a.gpuManager.GPUProcessSamples()
	}
	data.ContainerTelemetry = aggregateContainerTelemetry(identities, data.Processes, gpuProcesses)
	data.Connections, err = collectConnectionStats(ctx)
	if err != nil {
		return err
	}
	data.IRQs, err = collectIRQStats(ctx)
	return err
}

func containerIdentitiesFromStats(items []*container.Stats) []container.Identity {
	identities := make([]container.Identity, 0, len(items))
	for _, item := range items {
		if item == nil || (item.Id == "" && item.FullID == "") {
			continue
		}
		identities = append(identities, container.Identity{ID: item.Id, FullID: item.FullID, Name: item.Name})
	}
	return identities
}
