package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector"
	containermodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/live"
)

// liveSample is the result of one live collection together with the scope it
// was collected for. The scope is set when the run is created and never
// changes, so joiners can read it while the owner is still collecting.
type liveSample struct {
	includeDetails    bool
	includeContainers bool
	data              *systemmodel.CombinedData
	extras            live.Extras
}

// covers reports whether this sample's scope satisfies a request's scope.
func (s *liveSample) covers(includeDetails, includeContainers bool) bool {
	return (s.includeDetails || !includeDetails) && (s.includeContainers || !includeContainers)
}

type (
	liveRun        = collector.Run[liveSample]
	liveProcessRun = collector.Run[map[string]json.RawMessage]
)

// Live returns the LinuxIO-shaped live payload. Same request semantics as any
// live key: one-second reuse via liveCurrentData; a request during a collector
// pass waits on the app mutex.
func (a *App) Live(ctx context.Context) (monitoringapi.Live, error) {
	if err := ctx.Err(); err != nil {
		return monitoringapi.Live{}, err
	}
	run, err := a.liveCurrentRun(ctx, liveSampleKey(liveLiveEndpoint), true, true)
	if err != nil {
		return monitoringapi.Live{}, err
	}
	data := run.Value.data
	threads := 0
	if data.Details != nil {
		threads = data.Details.Threads
	}
	telemetry, telemetryAt := a.lastTelemetry()
	payload := live.Build(data, run.At, threads, telemetry, telemetryAt, 3*a.CollectorInterval())
	run.Value.extras.Apply(&payload)
	return payload, nil
}

func (a *App) lastTelemetry() ([]containermodel.Telemetry, time.Time) {
	a.telemetryMu.RLock()
	defer a.telemetryMu.RUnlock()
	return a.telemetry, a.telemetryAt
}

func (a *App) rememberTelemetry(items []containermodel.Telemetry, at time.Time) {
	a.telemetryMu.Lock()
	a.telemetry = items
	a.telemetryAt = at
	a.telemetryMu.Unlock()
}

// liveCurrentData returns a live sample for key. It reuses a finished sample
// under live.ReuseWindow old, joins an in-flight collection, and otherwise
// collects. The returned time is that sample's capture time, which callers
// report as captured_at. It never consults awaitCollectorSample: only the
// plugin, all and summary routes take that handoff, App.Live does not.
func (a *App) liveCurrentData(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*systemmodel.CombinedData, time.Time, error) {
	run, err := a.liveCurrentRun(ctx, key, includeDetails, includeContainers)
	if err != nil {
		return nil, time.Time{}, err
	}
	return run.Value.data, run.At, nil
}

func (a *App) liveCurrentRun(ctx context.Context, key uint16, includeDetails, includeContainers bool) (*liveRun, error) {
	seed := liveSample{includeDetails: includeDetails, includeContainers: includeContainers}
	return a.liveRuns.Do(ctx, key, seed,
		func(run *liveRun) bool {
			return run.Value.covers(includeDetails, includeContainers) && run.Fresh(live.ReuseWindow)
		},
		func(run *liveRun) { a.runLiveCollection(ctx, run, key, includeDetails, includeContainers) },
	)
}

// runLiveCollection collects into run. The deferred completion check runs
// before the run is published, so a collection that returns nothing still
// reports an error to its waiters.
func (a *App) runLiveCollection(ctx context.Context, run *liveRun, key uint16, includeDetails, includeContainers bool) {
	detachAfterCollection := a.collectLive != nil
	defer func() {
		if run.Value.data == nil && run.Err == nil {
			run.Err = errors.New("live collection did not complete")
		}
		if detachAfterCollection {
			a.detachLiveSample(run.Value.data)
		}
	}()

	collect := a.collectLive
	if collect == nil {
		collect = a.collectLiveCurrentData
	}
	run.Value.data, run.Err = collect(ctx, key, includeDetails, includeContainers)
	if run.Err == nil && key == liveLiveSampleKey {
		run.Value.extras = a.collectLiveExtras(ctx, run.Value.data)
		if run.Err == nil {
			run.Err = ctx.Err()
		}
	}
}

// collectLiveExtras gathers the optional sections that the live payload reads
// straight from the host.
func (a *App) collectLiveExtras(ctx context.Context, data *systemmodel.CombinedData) live.Extras {
	extras := live.Extras{
		Filesystems: a.filesystemInfo(ctx),
		Sensors:     live.Sensors(ctx, a.sensorTimeout()),
		Frequencies: data.Stats.CpuFrequencies,
		GPUs:        live.GPUs(ctx, data.Stats.GPUData),
	}
	if a.smartManager != nil {
		extras.Smart = a.liveSmartPower.Snapshot(ctx, a.smartManager.GetCurrentData())
	}
	return extras
}

func (a *App) filesystemInfo(ctx context.Context) []monitoringapi.FilesystemInfo {
	if a.fsManager == nil {
		return []monitoringapi.FilesystemInfo{}
	}
	return a.liveFilesystems.Info(ctx, a, a.fsManager)
}

func (a *App) sensorTimeout() time.Duration {
	if a.sensorConfig == nil {
		return 0
	}
	return a.sensorConfig.Timeout()
}

// detachLiveSample replaces the manager-owned pointers in a sample with
// pointers to value copies. Reuse can serve a sample up to live.ReuseWindow
// after capture, and both the docker container stats and the filesystem stats
// are rewritten in place by the next collection for any key; without this a
// reused sample would report another collection's values under its own
// captured_at. Both structs hold only value fields, so a shallow copy is a
// full one. No caller holds a.Lock when the publication defer runs, so it
// retakes that lock: another key's collection writes the same structs under it,
// and copying them unlocked would race that write.
func (a *App) detachLiveSample(data *systemmodel.CombinedData) {
	if data == nil {
		return
	}
	a.Lock()
	defer a.Unlock()
	a.detachLiveSampleLocked(data)
}

func (a *App) detachLiveSampleLocked(data *systemmodel.CombinedData) {
	if data == nil {
		return
	}
	for i, item := range data.Containers {
		if item != nil {
			copied := *item
			data.Containers[i] = &copied
		}
	}
	for key, stats := range data.Stats.ExtraFs {
		if stats != nil {
			copied := *stats
			data.Stats.ExtraFs[key] = &copied
		}
	}
}

// currentLiveProcessPayloads returns the live process, program and container
// telemetry payloads, sharing one collection per reuse window.
func (a *App) currentLiveProcessPayloads(ctx context.Context) (map[string]json.RawMessage, time.Time, error) {
	run, err := a.liveProcessRuns.Do(ctx, struct{}{}, nil,
		func(run *liveProcessRun) bool { return run.Fresh(live.ReuseWindow) },
		func(run *liveProcessRun) { a.runLiveProcessCollection(ctx, run) },
	)
	if err != nil {
		return nil, time.Time{}, err
	}
	return cloneJSONPayloads(run.Value), run.At, nil
}

func (a *App) runLiveProcessCollection(ctx context.Context, run *liveProcessRun) {
	defer func() {
		if run.Err == nil && run.Value == nil {
			run.Err = errors.New("live process collection did not complete")
		}
	}()
	if a.liveProcessCollect != nil {
		run.Value, run.Err = a.liveProcessCollect(ctx)
		return
	}
	identities, err := a.collectLiveContainerIdentities(ctx)
	if err != nil {
		run.Err = err
		return
	}
	run.Value, run.Err = a.collectProcessPluginPayloadsWithIdentities(ctx, identities)
}

func cloneJSONPayloads(payloads map[string]json.RawMessage) map[string]json.RawMessage {
	cloned := make(map[string]json.RawMessage, len(payloads))
	for key, payload := range payloads {
		cloned[key] = append(json.RawMessage(nil), payload...)
	}
	return cloned
}
