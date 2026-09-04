package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	procmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/process"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/store"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/utils"
)

var baseSystemPluginNames = []string{
	store.PluginCPU,
	store.PluginMem,
	store.PluginSwap,
	store.PluginLoad,
	store.PluginDiskIO,
	store.PluginFS,
	store.PluginNetwork,
	store.PluginGPU,
	store.PluginSensors,
}

var processPluginNames = []string{
	store.PluginProcesses,
	store.PluginPrograms,
	store.PluginContainerTelemetry,
}

const (
	liveSystemSummaryEndpoint = "system_summary"
	liveAllEndpoint           = "live_all"
	liveLiveEndpoint          = "live"
	liveSummarySampleKey      = uint16(1_000)
	liveAllSampleKey          = uint16(1_001)
	liveLiveSampleKey         = uint16(1_002)
	livePluginSampleKeyBase   = uint16(1_010)
	liveUnknownSampleKey      = uint16(1_999)
)

type liveProcessesData struct {
	Count *procmodel.Count    `json:"count,omitempty"`
	Items []procmodel.Process `json:"items"`
}

func (a *App) CurrentPlugin(ctx context.Context, plugin string) (int64, json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	if !store.IsPluginName(plugin) {
		return 0, nil, fmt.Errorf("unknown plugin %q", plugin)
	}
	if plugin != store.PluginSmart {
		if sample, reused, err := a.awaitCollectorSample(ctx); err != nil {
			return 0, nil, err
		} else if reused {
			raw, ok := sample.pluginPayload(plugin)
			if !ok {
				return 0, nil, fmt.Errorf("collector sample missing plugin %q", plugin)
			}
			return sample.capturedAt, raw, nil
		}
	}
	switch plugin {
	case store.PluginProcesses, store.PluginPrograms, store.PluginContainerTelemetry:
		return a.currentProcessPlugin(ctx, plugin)
	default:
		return a.collectCurrentPlugin(ctx, plugin)
	}
}

// CurrentPlugins collects a set of live plugins from one request. System
// plugins share one host sample, but no result is retained after this call.
func (a *App) CurrentPlugins(ctx context.Context, plugins []string) (int64, map[string]json.RawMessage, map[string]error) {
	batch := newCurrentPluginBatch(plugins)
	if err := ctx.Err(); err != nil {
		batch.failRequested(err)
		return 0, batch.raw, batch.errs
	}
	if sample, reused, err := a.awaitCollectorSample(ctx); err != nil {
		batch.failRequested(err)
		return 0, batch.raw, batch.errs
	} else if reused {
		a.fillBatchFromCollectorSample(sample, batch)
		return sample.capturedAt, batch.raw, batch.errs
	}

	var identities []container.Identity
	collectProcessBatch := batch.wantsAny(processPluginNames)
	if collectProcessBatch {
		var err error
		identities, err = a.collectLiveContainerIdentities(ctx)
		if err != nil {
			batch.failRequested(err, processPluginNames...)
			collectProcessBatch = false
		}
	}

	// Written by the single system-batch group and read after wg.Wait().
	var systemCapturedAt time.Time
	groups := []func(){
		func() { systemCapturedAt = a.collectCurrentSystemBatch(ctx, batch) },
		func() { a.collectCurrentStandaloneBatch(ctx, batch) },
	}
	if collectProcessBatch {
		groups = append(groups, func() { a.collectCurrentProcessBatch(ctx, batch, identities) })
	}
	runCurrentCollectionGroups(groups...)
	capturedAt := systemCapturedAt
	if capturedAt.IsZero() {
		capturedAt = time.Now()
	}
	return capturedAt.UTC().UnixMilli(), batch.raw, batch.errs
}

// fillBatchFromCollectorSample answers every requested plugin from a reused
// collector sample. SMART records are held by the app rather than the sample,
// so they are marshalled on the spot.
func (a *App) fillBatchFromCollectorSample(sample *collectorAPISample, batch *currentPluginBatch) {
	for plugin := range batch.requested {
		if plugin == store.PluginSmart {
			_, raw, smartErr := marshalCurrentPlugin(a.currentSmartRecords())
			if smartErr != nil {
				batch.setError(plugin, smartErr)
			} else {
				batch.setRaw(plugin, raw)
			}
			continue
		}
		raw, ok := sample.pluginPayload(plugin)
		if !ok {
			batch.setError(plugin, fmt.Errorf("collector sample missing plugin %q", plugin))
			continue
		}
		batch.setRaw(plugin, raw)
	}
}

type currentPluginBatch struct {
	mu        sync.Mutex
	requested map[string]bool
	raw       map[string]json.RawMessage
	errs      map[string]error
}

func newCurrentPluginBatch(plugins []string) *currentPluginBatch {
	batch := &currentPluginBatch{
		requested: make(map[string]bool, len(plugins)),
		raw:       make(map[string]json.RawMessage, len(plugins)),
		errs:      make(map[string]error),
	}
	for _, plugin := range plugins {
		if !store.IsPluginName(plugin) {
			batch.errs[plugin] = fmt.Errorf("unknown plugin %q", plugin)
			continue
		}
		batch.requested[plugin] = true
	}
	return batch
}

func (batch *currentPluginBatch) wantsAny(plugins []string) bool {
	for _, plugin := range plugins {
		if batch.requested[plugin] {
			return true
		}
	}
	return false
}

func (batch *currentPluginBatch) failRequested(err error, plugins ...string) {
	batch.mu.Lock()
	defer batch.mu.Unlock()

	if len(plugins) == 0 {
		for plugin := range batch.requested {
			batch.errs[plugin] = err
		}
		return
	}
	for _, plugin := range plugins {
		if batch.requested[plugin] {
			batch.errs[plugin] = err
		}
	}
}

func (batch *currentPluginBatch) addPayload(plugin string, payload any) {
	if payload == nil {
		batch.setError(plugin, fmt.Errorf("plugin %q returned no data", plugin))
		return
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		batch.setError(plugin, err)
		return
	}
	batch.setRaw(plugin, encoded)
}

func (batch *currentPluginBatch) setRaw(plugin string, raw json.RawMessage) {
	batch.mu.Lock()
	defer batch.mu.Unlock()
	batch.raw[plugin] = raw
}

func (batch *currentPluginBatch) setError(plugin string, err error) {
	batch.mu.Lock()
	defer batch.mu.Unlock()
	batch.errs[plugin] = err
}

// collectCurrentSystemBatch fills the system plugins from one live sample and
// returns that sample's capture time, or the zero time when no sample was taken.
func (a *App) collectCurrentSystemBatch(ctx context.Context, batch *currentPluginBatch) time.Time {
	includeContainers := batch.requested[store.PluginContainers]
	if !batch.wantsAny(baseSystemPluginNames) && !includeContainers {
		return time.Time{}
	}
	data, capturedAt, err := a.liveCurrentData(ctx, liveSampleKey(liveAllEndpoint), false, includeContainers)
	if err != nil {
		batch.failRequested(err, append(baseSystemPluginNames, store.PluginContainers)...)
		return time.Time{}
	}
	payloads := store.SnapshotPluginPayloads(data)
	for _, plugin := range append(baseSystemPluginNames, store.PluginContainers) {
		if batch.requested[plugin] {
			batch.addPayload(plugin, payloads[plugin])
		}
	}
	return capturedAt
}

func (a *App) collectCurrentProcessBatch(ctx context.Context, batch *currentPluginBatch, identities []container.Identity) {
	if !batch.wantsAny(processPluginNames) {
		return
	}
	payloads, err := a.collectProcessPluginPayloadsWithIdentities(ctx, identities)
	if err != nil {
		batch.failRequested(err, processPluginNames...)
		return
	}
	for _, plugin := range processPluginNames {
		if batch.requested[plugin] {
			batch.setRaw(plugin, payloads[plugin])
		}
	}
}

func (a *App) collectCurrentStandaloneBatch(ctx context.Context, batch *currentPluginBatch) {
	groups := make([]func(), 0, 3)
	for _, plugin := range []string{store.PluginConnections, store.PluginIRQ, store.PluginSmart} {
		if !batch.requested[plugin] {
			continue
		}
		groups = append(groups, func() {
			_, payload, err := a.collectCurrentPlugin(ctx, plugin)
			if err != nil {
				batch.setError(plugin, err)
			} else {
				batch.setRaw(plugin, payload)
			}
		})
	}
	runCurrentCollectionGroups(groups...)
}

func runCurrentCollectionGroups(groups ...func()) {
	var wg sync.WaitGroup
	for _, group := range groups {
		wg.Go(group)
	}
	wg.Wait()
}

func (a *App) SystemSummary(ctx context.Context) (int64, system.Summary, error) {
	if err := ctx.Err(); err != nil {
		return 0, system.Summary{}, err
	}
	if sample, reused, err := a.awaitCollectorSample(ctx); err != nil {
		return 0, system.Summary{}, err
	} else if reused {
		summary, summaryErr := sample.systemSummary()
		return sample.capturedAt, summary, summaryErr
	}
	data, capturedAt, err := a.liveCurrentData(ctx, liveSampleKey(liveSystemSummaryEndpoint), true, false)
	if err != nil {
		return 0, system.Summary{}, err
	}
	return capturedAt.UTC().UnixMilli(), system.NewSummary(data), nil
}

func (a *App) collectCurrentPlugin(ctx context.Context, plugin string) (int64, json.RawMessage, error) {
	switch plugin {
	case store.PluginConnections:
		stats, err := collectConnectionStats(ctx)
		if err != nil {
			return 0, nil, err
		}
		return marshalCurrentPlugin(stats)
	case store.PluginIRQ:
		stats, err := collectIRQStats(ctx)
		if err != nil {
			return 0, nil, err
		}
		return marshalCurrentPlugin(stats)
	case store.PluginSmart:
		if err := ctx.Err(); err != nil {
			return 0, nil, err
		}
		return marshalCurrentPlugin(a.currentSmartRecords())
	}

	sampleKey := liveSampleKey(plugin)
	return a.collectSystemPlugin(ctx, plugin, sampleKey, plugin == store.PluginContainers)
}

func (a *App) currentProcessPlugin(ctx context.Context, plugin string) (int64, json.RawMessage, error) {
	payloads, err := a.collectProcessPluginPayloads(ctx)
	if err != nil {
		return 0, nil, err
	}
	raw, ok := payloads[plugin]
	if !ok {
		return 0, nil, fmt.Errorf("unknown plugin %q", plugin)
	}
	return time.Now().UTC().UnixMilli(), raw, nil
}

func (a *App) collectProcessPluginPayloads(ctx context.Context) (map[string]json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	identities, err := a.collectLiveContainerIdentities(ctx)
	if err != nil {
		return nil, err
	}
	return a.collectProcessPluginPayloadsWithIdentities(ctx, identities)
}

func (a *App) collectLiveContainerIdentities(ctx context.Context) ([]container.Identity, error) {
	a.Lock()
	defer a.Unlock()

	var identities []container.Identity
	if a.dockerManager != nil {
		var err error
		identities, err = a.dockerManager.GetContainerIdentities(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			slog.Debug("Container identity collection failed", "err", err)
			identities = nil
		}
	}
	return identities, nil
}

func (a *App) collectProcessPluginPayloadsWithIdentities(ctx context.Context, identities []container.Identity) (map[string]json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	count, processes, programs, err := a.liveProcessManager.collectProcessStats(ctx, identities)
	if err != nil {
		return nil, err
	}
	var gpuProcesses []GPUProcessSample
	if a.gpuManager != nil {
		gpuProcesses = a.gpuManager.GPUProcessSamples()
	}
	containerTelemetry := aggregateContainerTelemetry(identities, processes, gpuProcesses)

	processRaw, err := json.Marshal(liveProcessesData{Count: count, Items: processes})
	if err != nil {
		return nil, err
	}
	programsRaw, err := json.Marshal(programs)
	if err != nil {
		return nil, err
	}
	containerTelemetryRaw, err := json.Marshal(containerTelemetry)
	if err != nil {
		return nil, err
	}
	return map[string]json.RawMessage{
		store.PluginProcesses:          processRaw,
		store.PluginPrograms:           programsRaw,
		store.PluginContainerTelemetry: containerTelemetryRaw,
	}, nil
}

func (a *App) collectLiveCurrentData(ctx context.Context, sampleKey uint16, includeDetails, includeContainers bool) (*system.CombinedData, error) {
	a.Lock()
	defer a.Unlock()

	stats, err := a.getSystemStats(ctx, sampleKey)
	if err != nil {
		return nil, err
	}
	data := &system.CombinedData{
		Stats: stats,
		Info:  a.systemInfoManager.systemInfo,
	}
	if includeContainers && a.dockerManager != nil {
		if containerStats, err := a.dockerManager.GetStats(ctx, sampleKey); err == nil {
			data.Containers = containerStats
		} else if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		} else {
			slog.Debug("Containers", "err", err)
		}
	}
	a.attachFilesystemStats(data)
	if includeDetails {
		details := a.systemInfoManager.systemDetails
		data.Details = &details
	}
	return data, nil
}

func (a *App) collectSystemPlugin(ctx context.Context, plugin string, sampleKey uint16, includeContainers bool) (int64, json.RawMessage, error) {
	data, capturedAt, err := a.liveCurrentData(ctx, sampleKey, false, includeContainers)
	if err != nil {
		return 0, nil, err
	}
	payloads := store.SnapshotPluginPayloads(data)
	payload, ok := payloads[plugin]
	if !ok {
		return 0, nil, fmt.Errorf("unknown plugin %q", plugin)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	return capturedAt.UTC().UnixMilli(), json.RawMessage(append([]byte(nil), raw...)), nil
}

func (a *App) attachFilesystemStats(data *system.CombinedData) {
	data.Stats.ExtraFs = make(map[string]*system.FsStats)
	data.Info.ExtraFsPct = make(map[string]float64)
	for name, stats := range a.fsManager.fsStats {
		if !stats.Root && stats.DiskTotal > 0 {
			key := name
			if stats.Name != "" {
				key = stats.Name
			}
			data.Stats.ExtraFs[key] = stats
			data.Info.ExtraFsPct[key] = utils.TwoDecimals((stats.DiskUsed / stats.DiskTotal) * 100)
		}
	}
}

func (a *App) currentSmartRecords() []store.SmartDeviceRecord {
	if a.smartManager == nil {
		return []store.SmartDeviceRecord{}
	}
	items := a.smartManager.GetCurrentData()
	records := make([]store.SmartDeviceRecord, 0, len(items))
	for key, item := range items {
		id := key
		if item.DiskName != "" {
			id = item.DiskName
		}
		records = append(records, store.SmartDeviceRecord{
			ID:   id,
			Key:  key,
			Data: item,
		})
	}
	return records
}

func marshalCurrentPlugin(payload any) (int64, json.RawMessage, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	return time.Now().UTC().UnixMilli(), raw, nil
}

// liveSampleKey gives each endpoint an independent delta baseline while
// keeping all live samples separate from the collector's 60-second key.
func liveSampleKey(endpoint string) uint16 {
	switch endpoint {
	case liveSystemSummaryEndpoint:
		return liveSummarySampleKey
	case liveAllEndpoint:
		return liveAllSampleKey
	case liveLiveEndpoint:
		return liveLiveSampleKey
	}
	sampleKey := livePluginSampleKeyBase
	for _, plugin := range store.PluginNames() {
		if endpoint == plugin {
			return sampleKey
		}
		sampleKey++
	}
	return liveUnknownSampleKey
}
