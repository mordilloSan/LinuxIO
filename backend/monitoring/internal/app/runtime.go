package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"runtime/pprof"
	"sync"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
	httpapi "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/api/http"
	apimodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/api/model"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/defaults"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/smart"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/store"
)

const (
	// maxHeaderValueCount caps how many header values the API parses per
	// request. The API only ever sees small GETs and command POSTs, so this
	// sits far below net/http's default of 500.
	maxHeaderValueCount = 64
)

// httpRuntime groups the HTTP server with its effective listen address. The
// effective address differs from the requested one when the caller asks for
// an ephemeral port (":0") and needs to discover what the kernel chose.
type httpRuntime struct {
	name             string
	requestedAddress string
	server           *http.Server
	effectiveAddress string
	apis             []string
	plugins          []string
}

type ListenerOptions struct {
	Name       string
	Address    string
	APIs       []string
	Mode       os.FileMode // unix sockets only; 0 means 0o660
	RootOnly   bool        // reject peers whose uid is not 0
	Plugins    []string    // nil means every metrics plugin
	BestEffort bool
}

type RunOptions struct {
	Listeners            []ListenerOptions
	CollectorInterval    time.Duration
	SmartRefreshInterval time.Duration
	DiskUsageCache       time.Duration
	HistoryRetention     time.Duration
	History              string
	HistorySet           bool
	HistoryIntervals     map[string]time.Duration // per-plugin history interval, a whole multiple of CollectorInterval
	ConfigPath           string
	ConfigSource         string
	ConfigVersion        int
	CommandExecutor      httpapi.CommandExecutor
	// ReloadConfig runs on SIGHUP. The hook re-reads and applies the
	// configuration itself (through ReloadRuntime) so the owner of the config
	// file can serialise it with its other config mutations.
	ReloadConfig func() error
}

type ReloadOptions struct {
	CollectorInterval    time.Duration
	SmartRefreshInterval time.Duration
	DiskUsageCache       time.Duration
	HistoryRetention     time.Duration
	History              string
	HistorySet           bool
	HistoryIntervals     map[string]time.Duration
	ConfigSource         string
	ConfigVersion        int
}

func (a *App) StartContext(ctx context.Context, opts RunOptions) error {
	if err := a.validateAndNormalizeOpts(&opts); err != nil {
		return err
	}

	historyPlugins, err := store.ParseHistoryPlugins(opts.History, opts.HistorySet)
	if err != nil {
		return err
	}
	historyEvery, err := store.HistoryEvery(opts.HistoryIntervals, opts.CollectorInterval)
	if err != nil {
		return err
	}
	a.setRuntimeConfig(opts.CollectorInterval, opts.SmartRefreshInterval, opts.HistoryRetention, historyPlugins, opts.HistoryIntervals, opts.ConfigPath, opts.ConfigSource, opts.ConfigVersion)
	a.applyDiskUsageCache(opts.DiskUsageCache)

	persistentStore, err := store.OpenStore(a.dataDir, store.Options{HistoryPlugins: historyPlugins, HistoryEvery: historyEvery, HistoryRetention: opts.HistoryRetention})
	if err != nil {
		return err
	}
	a.store = persistentStore
	slog.Info("Database ready", "path", persistentStore.Path())
	slog.Info("Collector configured",
		"interval", opts.CollectorInterval,
		"history", historyPlugins,
		"history_intervals", opts.HistoryIntervals,
		"smart_interval", a.smartRefreshIntervalString(),
	)
	defer func() {
		_ = a.store.Close()
		a.store = nil
	}()

	runCtx, cancelRun := context.WithCancel(ctx)
	var collectorWG sync.WaitGroup
	collectorStarted := false

	a.startManagers(runCtx)
	defer a.stopManagers()
	defer func() {
		cancelRun()
		if collectorStarted {
			collectorWG.Wait()
		}
	}()

	if collectErr := a.collectAndPersist(runCtx, time.Now().UTC()); collectErr != nil {
		return collectErr
	}

	serverErr := make(chan error, 1)
	if err := a.startHTTPServers(opts, serverErr); err != nil {
		if shutdownErr := a.shutdownHTTPServers(ctx); shutdownErr != nil {
			return errors.Join(err, shutdownErr)
		}
		return err
	}

	// The child context also stops the collector on the server-error path where
	// the caller's context may still be live.
	collectorIntervalUpdates := make(chan time.Duration, 1)
	a.runtimeMu.Lock()
	a.intervalUpdates = collectorIntervalUpdates
	a.runtimeMu.Unlock()
	defer func() {
		a.runtimeMu.Lock()
		a.intervalUpdates = nil
		a.runtimeMu.Unlock()
	}()
	collectorStarted = true
	collectorWG.Go(func() {
		pprof.Do(runCtx, pprof.Labels("component", "collector"), func(ctx context.Context) {
			a.runCollector(ctx, opts.CollectorInterval, collectorIntervalUpdates)
		})
	})

	hupCh := make(chan os.Signal, 1)
	signal.Notify(hupCh, syscall.SIGHUP)
	defer signal.Stop(hupCh)

	runErr := a.runEventLoop(runCtx, hupCh, serverErr, opts.ReloadConfig)

	if err := a.shutdownHTTPServers(ctx); err != nil {
		return err
	}
	return runErr
}

func (a *App) validateAndNormalizeOpts(opts *RunOptions) error {
	if a.dataDir == "" {
		return errors.New("data directory not configured")
	}
	if a.store != nil {
		return errors.New("agent already started")
	}
	if opts.CollectorInterval <= 0 {
		opts.CollectorInterval = defaults.CollectorInterval
	}
	if opts.SmartRefreshInterval <= 0 {
		opts.SmartRefreshInterval = defaults.SmartRefreshInterval
	}
	return nil
}

func (a *App) startHTTPServers(opts RunOptions, serverErr chan<- error) error {
	if len(opts.Listeners) == 0 {
		slog.Info("HTTP servers disabled")
		return nil
	}
	for _, listenerOpts := range opts.Listeners {
		if IsListenDisabled(listenerOpts.Address) {
			slog.Info("HTTP listener disabled", "name", listenerOpts.Name, "address", listenerOpts.Address)
			continue
		}
		listener, err := openListener(listenerOpts.Address, listenerOpts.Mode)
		if err != nil {
			if listenerOpts.BestEffort {
				slog.Warn("HTTP listener unavailable; continuing", "name", listenerOpts.Name, "address", listenerOpts.Address, "err", err)
				continue
			}
			return err
		}
		handler := a.apiServer(opts.CommandExecutor).HandlerFor(a.CollectorInterval, listenerOpts.APIs, listenerOpts.Plugins)
		var connContext func(context.Context, net.Conn) context.Context
		if listenerOpts.RootOnly {
			handler = requireRootPeer(handler)
			connContext = peercred.ConnContext
		}
		runtime := &httpRuntime{
			name:             listenerOpts.Name,
			requestedAddress: listenerOpts.Address,
			effectiveAddress: listener.Addr().String(),
			apis:             append([]string(nil), listenerOpts.APIs...),
			plugins:          append([]string(nil), listenerOpts.Plugins...),
			server: &http.Server{
				Addr:                listenerOpts.Address,
				Handler:             handler,
				ConnContext:         connContext,
				ReadHeaderTimeout:   5 * time.Second,
				MaxHeaderValueCount: maxHeaderValueCount,
			},
		}
		a.runtimeMu.Lock()
		a.httpRuntimes = append(a.httpRuntimes, runtime)
		a.runtimeMu.Unlock()
		slog.Info("HTTP server starting",
			"name", runtime.name,
			"address", runtime.effectiveAddress,
			"apis", runtime.apis,
			"request_logging", a.requestLogging,
		)
		go func(server *http.Server, name string) {
			// Labels are inherited by the per-connection goroutines Serve
			// spawns, so a leak or a stuck handler names its listener.
			pprof.Do(context.Background(), pprof.Labels("component", "http-server", "listener", name), func(context.Context) {
				if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
					serverErr <- err
				}
			})
		}(runtime.server, runtime.name)
	}
	return nil
}

func (a *App) shutdownHTTPServers(ctx context.Context) error {
	// Detach the runtimes before shutting them down: Shutdown waits for
	// in-flight handlers, and a handler reading /api/v1/meta takes the same
	// lock to list listeners.
	a.runtimeMu.Lock()
	runtimes := a.httpRuntimes
	a.httpRuntimes = nil
	a.runtimeMu.Unlock()

	var shutdownErr error
	for _, runtime := range runtimes {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		if err := runtime.server.Shutdown(shutdownCtx); err != nil {
			shutdownErr = errors.Join(shutdownErr, err)
		}
		cancel()
	}
	return shutdownErr
}

func (a *App) runEventLoop(ctx context.Context, hupCh <-chan os.Signal, serverErr <-chan error, reloadFn func() error) error {
	var runErr error
	running := true
	for running {
		select {
		case <-ctx.Done():
			slog.Info("Shutting down standalone agent")
			running = false
		case <-hupCh:
			a.handleSIGHUP(reloadFn)
		case err := <-serverErr:
			if err != nil {
				runErr = err
			}
			running = false
		}
	}
	return runErr
}

func (a *App) handleSIGHUP(reloadFn func() error) {
	if reloadFn == nil {
		slog.Info("SIGHUP received; no config reload hook configured")
		return
	}
	if err := reloadFn(); err != nil {
		slog.Error("Config reload failed", "err", err)
	}
}

func (a *App) ReloadRuntime(opts ReloadOptions) error {
	a.runtimeMu.RLock()
	updates := a.intervalUpdates
	a.runtimeMu.RUnlock()
	interval, historyPlugins, err := a.reloadRuntime(opts, updates)
	if err != nil {
		return err
	}
	slog.Info("Config reloaded",
		"interval", interval,
		"history", historyPlugins,
		"source", opts.ConfigSource,
		"version", opts.ConfigVersion,
	)
	return nil
}

func (a *App) reloadRuntime(opts ReloadOptions, updates chan time.Duration) (time.Duration, []string, error) {
	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()

	interval, historyPlugins, err := a.applyReload(opts)
	if err != nil {
		return 0, nil, err
	}
	replaceIntervalUpdate(updates, interval)
	return interval, historyPlugins, nil
}

func replaceIntervalUpdate(updates chan time.Duration, interval time.Duration) {
	if updates == nil {
		return
	}
	select {
	case <-updates:
	default:
	}
	select {
	case updates <- interval:
	default:
	}
}

func (a *App) startManagers(ctx context.Context) {
	if a.gpuManager != nil {
		if err := a.gpuManager.Start(ctx); err != nil {
			slog.Debug("GPU", "err", err)
			a.gpuManager.Stop()
		}
	}
}

func (a *App) stopManagers() {
	if a.gpuManager != nil {
		a.gpuManager.Stop()
	}
}

func (a *App) runCollector(ctx context.Context, interval time.Duration, intervalUpdates <-chan time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case nextInterval := <-intervalUpdates:
			if nextInterval <= 0 {
				nextInterval = defaults.CollectorInterval
			}
			ticker.Reset(nextInterval)
		case tickTime := <-ticker.C:
			if err := a.collectAndPersist(ctx, tickTime.UTC()); err != nil {
				slog.Error("collector tick failed", "err", err)
			}
		}
	}
}

func (a *App) collectAndPersist(ctx context.Context, now time.Time) error {
	run := a.beginCollectorRun()
	data, err := a.gatherStats(ctx, DataRequestOptions{
		CacheTimeMs:    collectorDataKeyMs,
		IncludeDetails: true,
	})
	if err != nil {
		a.finishCollectorRun(run, nil, err)
		return err
	}
	capturedAt := time.Now().UTC().UnixMilli()
	sample, err := newCollectorAPISample(capturedAt, data)
	if err != nil {
		a.finishCollectorRun(run, nil, err)
		return err
	}
	a.finishCollectorRun(run, sample, nil)

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.store.WriteSnapshot(capturedAt, data); err != nil {
		return err
	}
	a.lastCollectedMs.Store(capturedAt)
	a.rememberTelemetry(data.ContainerTelemetry, time.UnixMilli(capturedAt))

	// SMART refresh failures are logged when the refresh state changes so
	// persistent probe failures do not emit the same warning every cycle.
	_ = a.refreshSmartIfDue(ctx, now)
	if err := a.store.RunMaintenance(now); err != nil {
		return fmt.Errorf("maintenance failed: %w", err)
	}
	// Collection builds large transient process/network snapshots; release them
	// after the minute-scale tick so the agent's RSS stays close to baseline.
	debug.FreeOSMemory()
	return nil
}

func (a *App) refreshSmartIfDue(ctx context.Context, now time.Time) error {
	if a.smartManager == nil {
		return nil
	}

	a.Lock()
	shouldRefresh := a.smartManager.lastRefresh.IsZero() || now.Sub(a.smartManager.lastRefresh) >= a.smartManager.refreshInterval
	a.Unlock()
	if !shouldRefresh {
		return nil
	}

	err := a.refreshSmart(ctx, now, false)
	a.logSmartRefreshResult(err)
	return err
}

func (a *App) RefreshSmartNow(ctx context.Context) error {
	err := a.refreshSmart(ctx, time.Now().UTC(), true)
	a.logSmartRefreshResult(err)
	return err
}

func (a *App) refreshSmart(ctx context.Context, now time.Time, forceScan bool) error {
	if a.smartManager == nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return a.store.WriteSmartDevices(now.UnixMilli(), map[string]smart.SmartData{})
	}

	if err := a.smartManager.Refresh(ctx, forceScan); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.store.WriteSmartDevices(now.UnixMilli(), a.smartManager.GetCurrentData()); err != nil {
		return err
	}

	a.Lock()
	a.smartManager.lastRefresh = now
	a.Unlock()
	return nil
}

func (a *App) Listeners() []apimodel.ListenerMeta {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	out := make([]apimodel.ListenerMeta, 0, len(a.httpRuntimes))
	for _, runtime := range a.httpRuntimes {
		out = append(out, runtime.meta(true))
	}
	return out
}

func (r *httpRuntime) meta(active bool) apimodel.ListenerMeta {
	return apimodel.ListenerMeta{
		Name:             r.name,
		Address:          r.requestedAddress,
		EffectiveAddress: r.effectiveAddress,
		APIs:             append([]string(nil), r.apis...),
		Plugins:          append([]string(nil), r.plugins...),
		Active:           active,
	}
}

func (a *App) StatusMeta() apimodel.MetaResponse {
	dbPath := ""
	if a.store != nil {
		dbPath = a.store.Path()
	}
	var dbSize int64
	if files, err := store.DatabaseFiles(a.dataDir); err != nil {
		slog.Debug("database size unavailable", "err", err)
	} else {
		for _, file := range files {
			dbSize += file.Size
		}
	}
	return apimodel.MetaResponse{
		Version:              version.Version,
		DataDir:              a.dataDir,
		DBPath:               dbPath,
		DBSizeBytes:          dbSize,
		Listeners:            a.Listeners(),
		CollectorInterval:    a.CollectorInterval().String(),
		SmartRefreshInterval: a.smartRefreshIntervalString(),
		Config:               a.configInfo(),
		Retention: func() map[string]string {
			if a.store != nil {
				return a.store.RetentionStrings()
			}
			return store.RetentionStrings()
		}(),
	}
}

func (a *App) CheckDatabase() error {
	if a.store == nil {
		return errors.New("database not open")
	}
	return a.store.IntegrityCheck()
}

func (a *App) MaintainDatabase(ctx context.Context) error {
	if a.store == nil {
		return errors.New("database not open")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.store.RunMaintenance(time.Now().UTC()); err != nil {
		return err
	}
	return a.store.IntegrityCheck()
}

func (a *App) CollectorInterval() time.Duration {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	if a.collectorInterval <= 0 {
		return defaults.CollectorInterval
	}
	return a.collectorInterval
}

func (a *App) setRuntimeConfig(interval, smartRefreshInterval, retention time.Duration, historyPlugins []string, historyIntervals map[string]time.Duration, configPath, configSource string, configVersion int) {
	if interval <= 0 {
		interval = defaults.CollectorInterval
	}
	if smartRefreshInterval <= 0 {
		smartRefreshInterval = defaults.SmartRefreshInterval
	}
	if retention <= 0 {
		retention = store.DefaultHistoryRetention()
	}
	a.runtimeMu.Lock()
	a.collectorInterval = interval
	a.historyPlugins = append([]string(nil), historyPlugins...)
	a.historyIntervals = maps.Clone(historyIntervals)
	a.historyRetention = retention
	if a.store != nil {
		a.store.SetHistoryRetention(retention)
	}
	a.configPath = configPath
	a.configSource = configSource
	a.configVersion = configVersion
	a.runtimeMu.Unlock()

	a.setSmartRefreshInterval(smartRefreshInterval)
}

func (a *App) applyReload(opts ReloadOptions) (time.Duration, []string, error) {
	if opts.CollectorInterval <= 0 {
		opts.CollectorInterval = defaults.CollectorInterval
	}
	if opts.SmartRefreshInterval <= 0 {
		opts.SmartRefreshInterval = defaults.SmartRefreshInterval
	}
	if opts.HistoryRetention <= 0 {
		opts.HistoryRetention = store.DefaultHistoryRetention()
	}
	historyPlugins, err := store.ParseHistoryPlugins(opts.History, opts.HistorySet)
	if err != nil {
		return 0, nil, err
	}
	historyEvery, err := store.HistoryEvery(opts.HistoryIntervals, opts.CollectorInterval)
	if err != nil {
		return 0, nil, err
	}
	if a.store != nil {
		a.store.SetHistoryPlugins(historyPlugins)
		a.store.SetHistoryEvery(historyEvery)
	}
	a.runtimeMu.RLock()
	configPath := a.configPath
	a.runtimeMu.RUnlock()
	a.setRuntimeConfig(opts.CollectorInterval, opts.SmartRefreshInterval, opts.HistoryRetention, historyPlugins, opts.HistoryIntervals, configPath, opts.ConfigSource, opts.ConfigVersion)
	a.applyDiskUsageCache(opts.DiskUsageCache)
	return opts.CollectorInterval, historyPlugins, nil
}

// applyDiskUsageCache takes the app mutex the collector holds while reading
// fsManager state.
func (a *App) applyDiskUsageCache(d time.Duration) {
	a.Lock()
	defer a.Unlock()
	a.fsManager.setDiskUsageCache(d)
}

func (a *App) setSmartRefreshInterval(interval time.Duration) {
	if interval <= 0 {
		interval = defaults.SmartRefreshInterval
	}
	a.Lock()
	defer a.Unlock()
	a.systemInfoManager.systemDetails.SmartInterval = interval
	if a.smartManager != nil {
		a.smartManager.refreshInterval = interval
	}
}

func (a *App) configInfo() apimodel.ConfigMeta {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	intervals := make(map[string]string, len(a.historyIntervals))
	for plugin, interval := range a.historyIntervals {
		intervals[plugin] = interval.String()
	}
	return apimodel.ConfigMeta{
		Path:              a.configPath,
		Source:            a.configSource,
		Version:           a.configVersion,
		CollectorInterval: a.collectorInterval.String(),
		HistoryRetention:  a.historyRetention.String(),
		HistoryPlugins:    append([]string(nil), a.historyPlugins...),
		HistoryIntervals:  intervals,
	}
}

// LastCollected reports when the collector last persisted a sample.
func (a *App) LastCollected() (time.Time, bool) {
	ms := a.lastCollectedMs.Load()
	if ms == 0 {
		return time.Time{}, false
	}
	return time.UnixMilli(ms), true
}

func (a *App) apiServer(commandExecutor httpapi.CommandExecutor) *httpapi.Server {
	return httpapi.NewServer(httpapi.Options{
		Metrics:              a.store,
		Current:              a,
		SmartRefresher:       a,
		CommandExecutor:      commandExecutor,
		DataDir:              a.dataDir,
		Listeners:            a.Listeners,
		SmartRefreshInterval: a.smartRefreshIntervalString,
		ConfigInfo:           a.configInfo,
		LastCollected:        a.LastCollected,
		Live:                 a.Live,
		RequestLogging:       a.requestLogging,
	})
}

func (a *App) smartRefreshIntervalString() string {
	if a.smartManager == nil {
		return ""
	}
	a.Lock()
	defer a.Unlock()
	return a.smartManager.refreshInterval.String()
}

func (a *App) logSmartRefreshResult(err error) {
	if a.smartManager == nil {
		return
	}
	currentError := ""
	if err != nil {
		currentError = err.Error()
	}

	a.Lock()
	previousError := a.smartManager.lastRefreshError
	a.smartManager.lastRefreshError = currentError
	a.Unlock()

	switch {
	case currentError == "":
		if previousError != "" {
			slog.Info("smart refresh recovered")
		}
	case previousError != currentError:
		slog.Warn("smart refresh failed", "err", err)
	default:
		slog.Debug("smart refresh still failing", "err", err)
	}
}
